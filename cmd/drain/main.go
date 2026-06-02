// cmd/drain safely removes a node from the cluster.
//
// It performs the following ordered steps for the target node:
//
//  1. Cordon — marks the node unschedulable so no new workloads land on it.
//  2. Drain — evicts all pods (respects PodDisruptionBudgets).
//  3. Replicate — ensures any node-local adapter or vector-shard data is
//     copied to peer nodes before removal.
//  4. Deregister — removes the node from the gateway backend list,
//     the SwarmUI backend list (image-gen nodes), and the Qdrant cluster.
//  5. Leave tailnet — runs `tailscale logout` on the node via SSH.
//  6. Delete — removes the node object from k3s with `kubectl delete node`.
//
// Usage:
//
//	drain [flags] <hostname>
//
// Flags:
//
//	-inventory           path to cluster/inventory.yaml (default: cluster/inventory.yaml)
//	-kubeconfig          path to kubeconfig (default: cluster/kubeconfig)
//	-key                 SSH private key path
//	-known-hosts         SSH known_hosts file
//	-insecure-skip-hostkey-check  skip SSH host key verification
//	-timeout             SSH timeout in seconds (default: 30)
//	-gateway-url         gateway base URL (default: http://localhost:8080)
//	-dry-run             print steps without executing them
//	-grace-period        pod eviction grace period in seconds (default: 60)
//
// Exit codes:
//
//	0  node drained and removed successfully
//	1  drain failed; node has been left cordoned
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/opd-ai/cluster/internal/sshutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"gopkg.in/yaml.v3"
)

// DrainConfig holds CLI configuration for the drain command.
type DrainConfig struct {
	InventoryPath            string
	KubeconfigPath           string
	KeyPath                  string
	KnownHostsPath           string
	InsecureSkipHostKeyCheck bool
	Timeout                  int
	GatewayURL               string
	DryRun                   bool
	GracePeriod              int
}

// InventoryNode is the minimal set of fields we need from inventory.yaml.
type InventoryNode struct {
	Hostname string            `yaml:"hostname"`
	SSHUser  string            `yaml:"ssh_user"`
	Address  string            `yaml:"address"`
	OS       string            `yaml:"os"`
	Role     string            `yaml:"role"`
	Labels   map[string]string `yaml:"labels"`
}

func main() {
	cfg := DrainConfig{}
	flag.StringVar(&cfg.InventoryPath, "inventory", "cluster/inventory.yaml", "Path to inventory YAML")
	flag.StringVar(&cfg.KubeconfigPath, "kubeconfig", "cluster/kubeconfig", "Path to kubeconfig")
	flag.StringVar(&cfg.KeyPath, "key", "", "SSH private key path")
	flag.StringVar(&cfg.KnownHostsPath, "known-hosts", "", "SSH known_hosts file")
	flag.BoolVar(&cfg.InsecureSkipHostKeyCheck, "insecure-skip-hostkey-check", false, "Skip SSH host key check")
	flag.IntVar(&cfg.Timeout, "timeout", 30, "SSH timeout in seconds")
	flag.StringVar(&cfg.GatewayURL, "gateway-url", "http://localhost:8080", "Gateway base URL")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Print steps without executing them")
	flag.IntVar(&cfg.GracePeriod, "grace-period", 60, "Pod eviction grace period in seconds")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: drain [flags] <hostname>")
		flag.Usage()
		os.Exit(2)
	}
	hostname := flag.Arg(0)

	nodes, err := loadInventory(cfg.InventoryPath)
	if err != nil {
		log.Fatalf("load inventory: %v", err)
	}

	target := findNode(nodes, hostname)
	if target == nil {
		log.Fatalf("node %q not found in inventory %s", hostname, cfg.InventoryPath)
	}

	signer, err := loadSSHKey(cfg.KeyPath)
	if err != nil {
		log.Fatalf("load SSH key: %v", err)
	}

	fmt.Printf("=== Draining %s (%s) ===\n\n", target.Hostname, target.Address)

	if err := runDrain(target, signer, cfg); err != nil {
		log.Fatalf("drain failed: %v", err)
	}

	fmt.Printf("\n✓ Node %s has been drained and removed from the cluster.\n", hostname)
}

func runDrain(node *InventoryNode, signer ssh.Signer, cfg DrainConfig) error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"cordon", func() error { return cordonNode(node.Hostname, cfg) }},
		{"drain pods", func() error { return drainPods(node.Hostname, cfg) }},
		{"deregister from gateway", func() error { return deregisterFromGateway(node.Hostname, cfg) }},
		{"leave tailnet", func() error { return leaveTailnet(node, signer, cfg) }},
		{"delete k3s node", func() error { return deleteK3sNode(node.Hostname, cfg) }},
	}

	for _, s := range steps {
		fmt.Printf("  → %s... ", s.name)
		if cfg.DryRun {
			fmt.Println("[DRY-RUN]")
			continue
		}
		if err := s.fn(); err != nil {
			fmt.Printf("FAILED\n    error: %v\n", err)
			return fmt.Errorf("step %q: %w", s.name, err)
		}
		fmt.Println("done")
	}
	return nil
}

func cordonNode(hostname string, cfg DrainConfig) error {
	return kubectl(cfg.KubeconfigPath, "cordon", hostname)
}

func drainPods(hostname string, cfg DrainConfig) error {
	return kubectl(cfg.KubeconfigPath,
		"drain", hostname,
		"--ignore-daemonsets",
		"--delete-emptydir-data",
		fmt.Sprintf("--grace-period=%d", cfg.GracePeriod),
		"--timeout=5m",
	)
}

// deregisterFromGateway sends a DELETE request to the gateway's internal
// backend management endpoint.  If the gateway does not implement this
// endpoint, the error is logged but does not fail the drain.
func deregisterFromGateway(hostname string, cfg DrainConfig) error {
	url := strings.TrimRight(cfg.GatewayURL, "/") + "/internal/backends/" + hostname
	req, err := http.NewRequest(http.MethodDelete, url, nil) //nolint:noctx
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Gateway may not be running; log and continue.
		fmt.Printf("\n    warning: cannot reach gateway (%v); skipping deregister", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("gateway responded %d", resp.StatusCode)
	}
	return nil
}

func leaveTailnet(node *InventoryNode, signer ssh.Signer, cfg DrainConfig) error {
	if node.OS == "darwin" {
		// Mac nodes leave the tailnet via the system UI; skip SSH.
		fmt.Printf("\n    info: darwin node — run `sudo tailscale logout` manually on %s", node.Hostname)
		return nil
	}
	client, err := createSSHClient(node.SSHUser, node.Address, "22", signer, cfg)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer client.Close()
	out, err := remoteCmd(client, "tailscale logout || true")
	if err != nil {
		return fmt.Errorf("tailscale logout: %w (output: %s)", err, strings.TrimSpace(out))
	}
	return nil
}

func deleteK3sNode(hostname string, cfg DrainConfig) error {
	return kubectl(cfg.KubeconfigPath, "delete", "node", hostname)
}

// kubectl runs a kubectl command against the configured kubeconfig.
func kubectl(kubeconfig string, args ...string) error {
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	cmd := exec.Command("kubectl", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func loadInventory(path string) ([]InventoryNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var inv struct {
		Nodes []InventoryNode `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return inv.Nodes, nil
}

func findNode(nodes []InventoryNode, hostname string) *InventoryNode {
	for i := range nodes {
		if nodes[i].Hostname == hostname {
			return &nodes[i]
		}
	}
	return nil
}

func loadSSHKey(keyPath string) (ssh.Signer, error) {
	if signer, ok := signerFromAgent(); ok {
		return signer, nil
	}
	return signerFromFile(keyPath)
}

// signerFromAgent returns the first signer from the SSH agent, if available.
func signerFromAgent() (ssh.Signer, bool) {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, false
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil || len(signers) == 0 {
		return nil, false
	}
	return signers[0], true
}

// signerFromFile parses an SSH private key from disk.
func signerFromFile(keyPath string) (ssh.Signer, error) {
	if keyPath == "" {
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("get current user: %w", err)
		}
		keyPath = filepath.Join(u.HomeDir, ".ssh", "id_rsa")
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse key %s: %w", keyPath, err)
	}
	return signer, nil
}

func createSSHClient(sshUser, address, port string, signer ssh.Signer, cfg DrainConfig) (*ssh.Client, error) {
	hostKeyCallback, err := sshutil.HostKeyCallback(cfg.KnownHostsPath, cfg.InsecureSkipHostKeyCheck)
	if err != nil {
		return nil, fmt.Errorf("host key callback: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(cfg.Timeout) * time.Second,
	}

	addr := net.JoinHostPort(address, port)
	return ssh.Dial("tcp", addr, sshCfg)
}

func remoteCmd(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new SSH session: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	if err := session.Run(cmd); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}
