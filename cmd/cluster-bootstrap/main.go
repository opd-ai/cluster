package main

import (
	"bufio"
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
)

type BootstrapConfig struct {
	InventoryPath            string
	DryRun                   bool
	KeyPath                  string
	KnownHostsPath           string
	InsecureSkipHostKeyCheck bool
	Timeout                  int
	UpMode                   bool
}

type NodeConfig struct {
	Hostname    string
	SSHUser     string
	Address     string
	Arch        string
	OS          string
	Accelerator string
	Role        string
	Labels      map[string]string
}

func main() {
	config := BootstrapConfig{}
	flag.StringVar(&config.InventoryPath, "inventory", "cluster/inventory.yaml", "Path to inventory YAML file")
	flag.BoolVar(&config.DryRun, "dry-run", false, "Show what would be done without doing it")
	flag.StringVar(&config.KeyPath, "key", "", "SSH private key path")
	flag.StringVar(&config.KnownHostsPath, "known-hosts", "", "SSH known_hosts file (default: ~/.ssh/known_hosts)")
	flag.BoolVar(&config.InsecureSkipHostKeyCheck, "insecure-skip-hostkey-check", false, "Skip SSH host key verification")
	flag.IntVar(&config.Timeout, "timeout", 30, "SSH timeout in seconds")
	flag.BoolVar(&config.UpMode, "up", false, "Full bring-up mode (k3s cluster + services)")
	flag.Parse()

	// Load inventory
	nodes, err := loadInventory(config.InventoryPath)
	if err != nil {
		log.Fatalf("Failed to load inventory: %v", err)
	}

	if len(nodes) == 0 {
		log.Fatal("No nodes in inventory")
	}

	// Load SSH key
	signer, err := loadSSHKey(config.KeyPath)
	if err != nil {
		log.Fatalf("Failed to load SSH key: %v", err)
	}

	// Bootstrap each node
	for _, node := range nodes {
		func(node NodeConfig) {
			fmt.Printf("\n=== Bootstrapping %s (%s) ===\n", node.Hostname, node.Address)

			client, err := createSSHClient(node.SSHUser, node.Address, "22", signer, config)
			if err != nil {
				log.Printf("Failed to connect to %s: %v", node.Hostname, err)
				return
			}
			defer client.Close()

			// Test connection
			if output, err := remoteCmd(client, "echo 'Connected'"); err != nil || strings.TrimSpace(output) != "Connected" {
				log.Printf("Connection test failed on %s", node.Hostname)
				return
			}

			// Run bootstrap steps
			if err := bootstrapNode(client, &node, config); err != nil {
				log.Printf("Bootstrap failed on %s: %v", node.Hostname, err)
				return
			}

			fmt.Printf("✓ %s bootstrapped successfully\n", node.Hostname)
		}(node)
	}

	if config.UpMode {
		fmt.Println("\nNote: Full cluster bring-up (k3s control-plane + workers) is not yet implemented.")
		fmt.Println("Run 'make up' after bootstrapping to complete cluster formation.")
	}
}

func loadInventory(path string) ([]NodeConfig, error) {
	// Simple YAML parsing for our specific inventory format
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var nodes []NodeConfig
	var current NodeConfig
	inLabels := false
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "nodes:" {
			continue
		}

		if strings.HasPrefix(trimmed, "- hostname:") {
			if current.Hostname != "" {
				nodes = append(nodes, current)
			}
			current = NodeConfig{Labels: map[string]string{}}
			current.Hostname = strings.TrimSpace(strings.TrimPrefix(trimmed, "- hostname:"))
			inLabels = false
			continue
		}

		if trimmed == "labels:" {
			inLabels = true
			if current.Labels == nil {
				current.Labels = map[string]string{}
			}
			continue
		}

		if inLabels {
			if indent >= 6 {
				key, value, ok := strings.Cut(trimmed, ":")
				if ok {
					current.Labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
				}
				continue
			}
			inLabels = false
		}

		parseInventoryField(&current, trimmed)
	}

	if current.Hostname != "" {
		nodes = append(nodes, current)
	}

	return nodes, scanner.Err()
}

func parseInventoryField(node *NodeConfig, line string) {
	if strings.HasPrefix(line, "ssh_user:") {
		node.SSHUser = strings.TrimSpace(strings.TrimPrefix(line, "ssh_user:"))
	} else if strings.HasPrefix(line, "address:") {
		node.Address = strings.TrimSpace(strings.TrimPrefix(line, "address:"))
	} else if strings.HasPrefix(line, "arch:") {
		node.Arch = strings.TrimSpace(strings.TrimPrefix(line, "arch:"))
	} else if strings.HasPrefix(line, "os:") {
		node.OS = strings.TrimSpace(strings.TrimPrefix(line, "os:"))
	} else if strings.HasPrefix(line, "accelerator:") {
		node.Accelerator = strings.TrimSpace(strings.TrimPrefix(line, "accelerator:"))
	} else if strings.HasPrefix(line, "role:") {
		node.Role = strings.TrimSpace(strings.TrimPrefix(line, "role:"))
	}
}

func loadSSHKey(keyPath string) (ssh.Signer, error) {
	if keyPath == "" {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		} else {
			keyPath = filepath.Join(usr.HomeDir, ".ssh", "id_rsa")
		}
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return getAgentSigner()
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return getAgentSigner()
	}
	return signer, nil
}

func getAgentSigner() (ssh.Signer, error) {
	agentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent: %v", err)
	}

	ag := agent.NewClient(agentConn)
	signers, err := ag.Signers()
	if err != nil {
		return nil, fmt.Errorf("failed to get signers from agent: %v", err)
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no signers available in SSH agent")
	}
	return signers[0], nil
}

func createSSHClient(user, address, port string, signer ssh.Signer, bootstrapConfig BootstrapConfig) (*ssh.Client, error) {
	hostKeyCallback, err := sshutil.HostKeyCallback(bootstrapConfig.KnownHostsPath, bootstrapConfig.InsecureSkipHostKeyCheck)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(bootstrapConfig.Timeout) * time.Second,
	}

	return ssh.Dial("tcp", address+":"+port, config)
}

func remoteCmd(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
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

func bootstrapNode(client *ssh.Client, node *NodeConfig, config BootstrapConfig) error {
	// Detect OS/distro
	osRelease, err := remoteCmd(client, "cat /etc/os-release 2>/dev/null || cat /etc/issue 2>/dev/null || echo 'unknown'")
	if err != nil {
		return fmt.Errorf("failed to detect OS")
	}

	// Route to OS-specific bootstrap
	if node.OS == "darwin" || strings.Contains(osRelease, "Darwin") {
		return bootstrapMacOS(client, node, config)
	} else if isUbuntuDebian(osRelease) {
		return bootstrapUbuntuDebian(client, node, config)
	} else if isRHEL(osRelease) {
		return bootstrapRHEL(client, node, config)
	}

	return fmt.Errorf("unsupported OS")
}

func bootstrapMacOS(client *ssh.Client, node *NodeConfig, config BootstrapConfig) error {
	steps := []string{
		"command -v brew >/dev/null 2>&1 || /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"",
		"brew install ollama git git-lfs rsync curl wget",
	}

	// MLX runtime for Apple Silicon
	if node.Arch == "arm64" {
		steps = append(steps,
			"pip3 install mlx-lm mlx-vlm || true",
			"brew install python@3.11 || true",
		)
	}

	// Python and uv for trainers
	if isTrainerNode(node) {
		steps = append(steps,
			"brew install python@3.11",
			"curl -LsSf https://astral.sh/uv/install.sh | sh || pip3 install uv",
			"brew install pytorch::pytorch || true",
		)
	}

	steps = append(steps, "brew services start ollama || launchctl start homebrew.mxcl.ollama || true")

	return executeBootstrapSteps(client, steps, config)
}

func bootstrapUbuntuDebian(client *ssh.Client, node *NodeConfig, config BootstrapConfig) error {
	steps := []string{
		"apt-get update",
		"apt-get install -y curl wget git git-lfs rsync ca-certificates",
		"apt-get install -y containerd.io || apt-get install -y docker.io",
		"systemctl enable containerd || systemctl enable docker",
		"systemctl start containerd || systemctl start docker",
	}

	if node.Accelerator == "cuda" {
		steps = append(steps,
			"apt-get install -y nvidia-driver-550 || apt-get install -y nvidia-driver-latest",
			"apt-get install -y nvidia-container-toolkit",
			"nvidia-ctk runtime configure --runtime=containerd",
		)
	} else if node.Accelerator == "rocm" {
		steps = append(steps,
			"apt-get install -y rocm-driver-core rocm-opencl-runtime",
			"apt-get install -y rocm-container-runtime || true",
		)
	}

	steps = append(steps,
		"curl -fsSL https://ollama.ai/install.sh | sh || true",
		"systemctl enable ollama || true",
		"systemctl start ollama || true",
	)

	if isTrainerNode(node) {
		steps = append(steps,
			"apt-get install -y python3-dev python3-pip python3.11-dev",
			"curl -LsSf https://astral.sh/uv/install.sh | sh || pip3 install uv",
		)
	}

	return executeBootstrapSteps(client, steps, config)
}

func bootstrapRHEL(client *ssh.Client, node *NodeConfig, config BootstrapConfig) error {
	steps := []string{
		"yum install -y curl wget git git-lfs rsync ca-certificates",
		"yum install -y containerd || yum install -y docker",
		"systemctl enable containerd || systemctl enable docker",
		"systemctl start containerd || systemctl start docker",
	}

	if node.Accelerator == "cuda" {
		steps = append(steps,
			"yum install -y nvidia-driver || true",
			"yum install -y nvidia-container-toolkit || true",
		)
	} else if node.Accelerator == "rocm" {
		steps = append(steps,
			"yum install -y rocm-opencl rocm-opencl-devel || true",
		)
	}

	steps = append(steps,
		"curl -fsSL https://ollama.ai/install.sh | sh || true",
		"systemctl enable ollama || true",
		"systemctl start ollama || true",
	)

	if isTrainerNode(node) {
		steps = append(steps,
			"yum install -y python3-devel",
			"curl -LsSf https://astral.sh/uv/install.sh | sh || pip3 install uv",
		)
	}

	return executeBootstrapSteps(client, steps, config)
}

func executeBootstrapSteps(client *ssh.Client, steps []string, config BootstrapConfig) error {
	for _, step := range steps {
		if config.DryRun {
			fmt.Printf("  [DRY-RUN] %s\n", step)
			continue
		}

		fmt.Printf("  → %s\n", step)
		output, err := remoteCmd(client, step)
		if err != nil && !isIdempotentError(step, output) {
			fmt.Printf("    Warning: %v\n", err)
			if output != "" {
				fmt.Printf("    Output: %s\n", strings.TrimSpace(output))
			}
		}
	}
	return nil
}

func isUbuntuDebian(osRelease string) bool {
	return strings.Contains(osRelease, "Ubuntu") || strings.Contains(osRelease, "Debian") ||
		strings.Contains(osRelease, "ubuntu") || strings.Contains(osRelease, "debian")
}

func isRHEL(osRelease string) bool {
	return strings.Contains(osRelease, "Red Hat") || strings.Contains(osRelease, "CentOS") ||
		strings.Contains(osRelease, "Fedora") || strings.Contains(osRelease, "rhel")
}

func isTrainerNode(node *NodeConfig) bool {
	return strings.EqualFold(strings.TrimSpace(node.Labels["workload"]), "trainer")
}

func isIdempotentError(step, output string) bool {
	// These errors are OK if the software is already installed
	return strings.Contains(output, "already") ||
		strings.Contains(output, "already installed") ||
		strings.Contains(output, "is already the newest version") ||
		strings.Contains(output, "nothing to do")
}
