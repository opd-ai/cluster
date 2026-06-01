// cmd/cluster-join generates one-shot k3s join tokens and prints a ready-to-run
// join script for each Linux worker node listed in the inventory.
//
// Usage:
//
//	cluster-join [flags]
//
// Flags:
//
//	-inventory   path to cluster/inventory.yaml (default: cluster/inventory.yaml)
//	-control     address of the k3s control node (overrides inventory lookup)
//	-key         SSH private key path
//	-known-hosts SSH known_hosts file
//	-insecure-skip-hostkey-check  skip host key verification
//	-timeout     SSH timeout in seconds (default: 30)
//	-script      write per-worker join scripts to this directory instead of stdout
//
// Mac (os: darwin) nodes are always skipped — they join as external workers
// via launchd, not k3s.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/opd-ai/cluster/internal/sshutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"gopkg.in/yaml.v3"
)

// JoinConfig holds CLI configuration for cluster-join.
type JoinConfig struct {
	InventoryPath            string
	ControlAddress           string
	KeyPath                  string
	KnownHostsPath           string
	InsecureSkipHostKeyCheck bool
	Timeout                  int
	ScriptDir                string
}

// InventoryNode is a minimal representation of an inventory entry.
type InventoryNode struct {
	Hostname string `yaml:"hostname"`
	SSHUser  string `yaml:"ssh_user"`
	Address  string `yaml:"address"`
	OS       string `yaml:"os"`
	Role     string `yaml:"role"`
}

func main() {
	cfg := JoinConfig{}
	flag.StringVar(&cfg.InventoryPath, "inventory", "cluster/inventory.yaml", "Path to inventory YAML")
	flag.StringVar(&cfg.ControlAddress, "control", "", "Address of the k3s control node (overrides inventory)")
	flag.StringVar(&cfg.KeyPath, "key", "", "SSH private key path")
	flag.StringVar(&cfg.KnownHostsPath, "known-hosts", "", "SSH known_hosts file")
	flag.BoolVar(&cfg.InsecureSkipHostKeyCheck, "insecure-skip-hostkey-check", false, "Skip host key check")
	flag.IntVar(&cfg.Timeout, "timeout", 30, "SSH timeout in seconds")
	flag.StringVar(&cfg.ScriptDir, "script", "", "Write join scripts here instead of stdout")
	flag.Parse()

	nodes, err := loadInventory(cfg.InventoryPath)
	if err != nil {
		log.Fatalf("load inventory: %v", err)
	}

	controlAddr := cfg.ControlAddress
	if controlAddr == "" {
		cn := findControl(nodes)
		if cn == nil {
			log.Fatal("no node with role=control in inventory; use -control to specify address")
		}
		controlAddr = cn.Address
	}

	signer, err := loadSSHKey(cfg.KeyPath)
	if err != nil {
		log.Fatalf("load SSH key: %v", err)
	}

	token, err := fetchToken(controlAddr, "root", signer, cfg)
	if err != nil {
		// Try the control node's ssh_user if root fails.
		cn := findControl(nodes)
		if cn != nil && cn.SSHUser != "" && cn.SSHUser != "root" {
			token, err = fetchToken(controlAddr, cn.SSHUser, signer, cfg)
		}
		if err != nil {
			log.Fatalf("fetch node-token from control node: %v", err)
		}
	}

	for _, node := range nodes {
		if node.OS == "darwin" {
			fmt.Printf("skip  %s (darwin — joins as external worker via launchd)\n", node.Hostname)
			continue
		}
		if node.Role != "worker" && node.Role != "both" {
			continue
		}
		script, err := joinScript(controlAddr, token)
		if err != nil {
			log.Fatalf("joinScript: %v", err)
		}
		if cfg.ScriptDir != "" {
			if err := writeScript(cfg.ScriptDir, node.Hostname, script); err != nil {
				log.Printf("write script for %s: %v", node.Hostname, err)
			} else {
				fmt.Printf("wrote join script: %s/%s-join.sh\n", cfg.ScriptDir, node.Hostname)
			}
		} else {
			fmt.Printf("# --- join script for %s (%s) ---\n%s\n", node.Hostname, node.Address, script)
		}
	}
}

// fetchToken SSHes to the control node and reads /var/lib/rancher/k3s/server/node-token.
func fetchToken(address, sshUser string, signer ssh.Signer, cfg JoinConfig) (string, error) {
	client, err := dialSSH(sshUser, address, "22", signer, cfg)
	if err != nil {
		return "", err
	}
	defer client.Close()

	out, err := remoteCmd(client, "cat /var/lib/rancher/k3s/server/node-token")
	if err != nil {
		return "", fmt.Errorf("read node-token: %w", err)
	}
	token := strings.TrimSpace(out)
	if token == "" {
		return "", fmt.Errorf("node-token is empty (is k3s running?)")
	}
	return token, nil
}

// shellSafe reports whether s is safe to embed verbatim in a shell script:
// it must be non-empty and contain only alphanumeric characters, dots,
// hyphens, colons, underscores, and forward slashes.
func shellSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '-' ||
			c == ':' || c == '_' || c == '/') {
			return false
		}
	}
	return true
}

// joinScript returns an idempotent shell script that installs and starts the
// k3s agent on a worker node.
func joinScript(serverAddr, token string) (string, error) {
	if !shellSafe(serverAddr) {
		return "", fmt.Errorf("joinScript: serverAddr contains unsafe characters")
	}
	if !shellSafe(token) {
		return "", fmt.Errorf("joinScript: token contains unsafe characters")
	}
	return fmt.Sprintf(`#!/usr/bin/env sh
set -eu
curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=%s sh -
systemctl enable k3s-agent
systemctl start k3s-agent
echo "joined k3s cluster at %s"
`, serverAddr, token, serverAddr), nil
}

// writeScript writes a join script for hostname into dir.
func writeScript(dir, hostname, script string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, hostname+"-join.sh")
	return os.WriteFile(path, []byte(script), 0o750)
}

// findControl returns the first node with role control or both.
func findControl(nodes []InventoryNode) *InventoryNode {
	for i := range nodes {
		if nodes[i].Role == "control" || nodes[i].Role == "both" {
			return &nodes[i]
		}
	}
	return nil
}

func loadInventory(path string) ([]InventoryNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv struct {
		Nodes []InventoryNode `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse inventory %s: %w", path, err)
	}
	var nodes []InventoryNode
	for _, n := range inv.Nodes {
		if n.Hostname == "" {
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func loadSSHKey(keyPath string) (ssh.Signer, error) {
	if keyPath == "" {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		}
		keyPath = filepath.Join(usr.HomeDir, ".ssh", "id_rsa")
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return agentSigner()
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return agentSigner()
	}
	return signer, nil
}

func agentSigner() (ssh.Signer, error) {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, fmt.Errorf("SSH agent unavailable: %w", err)
	}
	defer conn.Close()
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil, err
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no signers in SSH agent")
	}
	return signers[0], nil
}

func dialSSH(sshUser, address, port string, signer ssh.Signer, cfg JoinConfig) (*ssh.Client, error) {
	cb, err := sshutil.HostKeyCallback(cfg.KnownHostsPath, cfg.InsecureSkipHostKeyCheck)
	if err != nil {
		return nil, err
	}
	c := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: cb,
		Timeout:         time.Duration(cfg.Timeout) * time.Second,
	}
	return ssh.Dial("tcp", address+":"+port, c)
}

func remoteCmd(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var buf bytes.Buffer
	sess.Stdout = &buf
	if err := sess.Run(cmd); err != nil {
		return "", err
	}
	return buf.String(), nil
}
