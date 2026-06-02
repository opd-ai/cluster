package main

import (
	"bytes"
	"errors"
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
	Hostname    string            `yaml:"hostname"`
	SSHUser     string            `yaml:"ssh_user"`
	Address     string            `yaml:"address"`
	Arch        string            `yaml:"arch"`
	OS          string            `yaml:"os"`
	Accelerator string            `yaml:"accelerator"`
	Role        string            `yaml:"role"`
	Labels      map[string]string `yaml:"labels"`
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
		if err := bringUpCluster(nodes, signer, config); err != nil {
			log.Fatalf("Cluster bring-up failed: %v", err)
		}
	}
}

// bringUpCluster installs k3s on the control node, exports kubeconfig, and
// joins Linux worker nodes.  Mac (darwin) nodes are skipped — they run our
// Go agents natively under launchd and do not join k3s.
func bringUpCluster(nodes []NodeConfig, signer ssh.Signer, config BootstrapConfig) error {
	controlNode := findControlNode(nodes)
	if controlNode == nil {
		return fmt.Errorf("no node with role=control found in inventory")
	}

	fmt.Printf("\n=== Installing k3s control-plane on %s ===\n", controlNode.Hostname)

	client, err := createSSHClient(controlNode.SSHUser, controlNode.Address, "22", signer, config)
	if err != nil {
		return fmt.Errorf("connect to control node: %w", err)
	}
	defer client.Close()

	if err := installK3sServer(client, config); err != nil {
		return fmt.Errorf("k3s server install: %w", err)
	}

	kubeconfig, err := fetchKubeconfig(client)
	if err != nil {
		return fmt.Errorf("fetch kubeconfig: %w", err)
	}

	if err := saveKubeconfig(config.InventoryPath, kubeconfig, controlNode.Address); err != nil {
		return fmt.Errorf("save kubeconfig: %w", err)
	}

	token, err := fetchNodeToken(client)
	if err != nil {
		return fmt.Errorf("fetch node token: %w", err)
	}

	for _, node := range nodes {
		if node.Hostname == controlNode.Hostname || node.OS == "darwin" {
			continue
		}
		if !isWorkerNode(&node) {
			continue
		}
		fmt.Printf("\n=== Joining worker %s ===\n", node.Hostname)
		wc, err := createSSHClient(node.SSHUser, node.Address, "22", signer, config)
		if err != nil {
			log.Printf("Failed to connect to worker %s: %v", node.Hostname, err)
			continue
		}
		if err := joinK3sWorker(wc, controlNode.Address, token, config); err != nil {
			log.Printf("Failed to join worker %s: %v", node.Hostname, err)
		} else {
			fmt.Printf("✓ %s joined the cluster\n", node.Hostname)
		}
		wc.Close()
	}

	fmt.Printf("\n✓ k3s control-plane is up. Kubeconfig saved.\n")
	fmt.Printf("\nTo join additional workers manually:\n")
	sshUser := controlNode.SSHUser
	if sshUser == "" {
		sshUser = "root"
	}
	fmt.Printf("  TOKEN=$(ssh %s@%s 'sudo cat /var/lib/rancher/k3s/server/node-token')\n",
		sshUser, controlNode.Address)
	fmt.Printf("  curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=\"$TOKEN\" sh -\n\n",
		controlNode.Address)
	return nil
}

func findControlNode(nodes []NodeConfig) *NodeConfig {
	for i := range nodes {
		if nodes[i].Role == "control" || nodes[i].Role == "both" {
			return &nodes[i]
		}
	}
	return nil
}

func isWorkerNode(node *NodeConfig) bool {
	return node.Role == "worker" || node.Role == "both"
}

func installK3sServer(client *ssh.Client, config BootstrapConfig) error {
	steps := []string{
		"curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC='server' sh -s -",
		"systemctl enable k3s",
		"systemctl start k3s",
		"until kubectl get nodes 2>/dev/null | grep -q 'Ready'; do sleep 2; done",
	}
	return executeBootstrapSteps(client, steps, config)
}

func fetchKubeconfig(client *ssh.Client) (string, error) {
	out, err := remoteCmd(client, "cat /etc/rancher/k3s/k3s.yaml")
	if err != nil {
		return "", fmt.Errorf("read k3s.yaml: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("k3s.yaml is empty")
	}
	return out, nil
}

// saveKubeconfig writes kubeconfig adjacent to the inventory file, replacing
// the loopback server address with the control node's actual address.
func saveKubeconfig(inventoryPath, kubeconfig, controlAddr string) error {
	dir := filepath.Dir(inventoryPath)
	dst := filepath.Join(dir, "kubeconfig")
	// Replace the default 127.0.0.1 server address so external clients work.
	kubeconfig = strings.ReplaceAll(kubeconfig, "127.0.0.1", controlAddr)
	if err := os.WriteFile(dst, []byte(kubeconfig), 0o600); err != nil {
		return err
	}
	fmt.Printf("Kubeconfig written to %s\n", dst)
	return nil
}

func fetchNodeToken(client *ssh.Client) (string, error) {
	out, err := remoteCmd(client, "cat /var/lib/rancher/k3s/server/node-token")
	if err != nil {
		return "", fmt.Errorf("read node-token: %w", err)
	}
	token := strings.TrimSpace(out)
	if token == "" {
		return "", fmt.Errorf("node-token is empty")
	}
	return token, nil
}

func joinK3sWorker(client *ssh.Client, serverAddr, token string, config BootstrapConfig) error {
	if !shellSafe(serverAddr) {
		return fmt.Errorf("joinK3sWorker: serverAddr contains unsafe characters")
	}
	if !shellSafe(token) {
		return fmt.Errorf("joinK3sWorker: token contains unsafe characters")
	}
	cmd := fmt.Sprintf(
		"curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=%s sh -",
		serverAddr, token,
	)
	steps := []string{
		cmd,
		"systemctl enable k3s-agent",
		"systemctl start k3s-agent",
	}
	return executeBootstrapSteps(client, steps, config)
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

func loadInventory(path string) ([]NodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv struct {
		Nodes []NodeConfig `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse inventory %s: %w", path, err)
	}
	var nodes []NodeConfig
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
	defer agentConn.Close()

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
		// Tailscale for mesh networking — Mac nodes join the tailnet so the Go
		// agents are reachable at a stable identity across NAT.
		"brew install --cask tailscale || true",
		"open -a Tailscale || true",
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

	// Tailscale — install via official script then enable for stable tailnet identity.
	steps = append(steps,
		"curl -fsSL https://tailscale.com/install.sh | sh || true",
		"systemctl enable tailscaled || true",
		"systemctl start tailscaled || true",
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

	// Tailscale — install via official script then enable for stable tailnet identity.
	steps = append(steps,
		"curl -fsSL https://tailscale.com/install.sh | sh || true",
		"systemctl enable tailscaled || true",
		"systemctl start tailscaled || true",
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
	var errs []error
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
			errs = append(errs, fmt.Errorf("step %q: %w", step, err))
		}
	}
	return errors.Join(errs...)
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
	return strings.Contains(output, "already installed") ||
		strings.Contains(output, "is already the newest version") ||
		strings.Contains(output, "already exists") ||
		strings.Contains(output, "nothing to do")
}
