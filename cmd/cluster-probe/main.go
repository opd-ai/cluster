package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opd-ai/cluster/internal/sshutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Node struct {
	Hostname    string            `yaml:"hostname" json:"hostname"`
	SSHUser     string            `yaml:"ssh_user" json:"ssh_user"`
	Address     string            `yaml:"address" json:"address"`
	Arch        string            `yaml:"arch" json:"arch"`
	OS          string            `yaml:"os" json:"os"`
	Role        string            `yaml:"role" json:"role"`
	Accelerator string            `yaml:"accelerator" json:"accelerator"`
	VramGB      int               `yaml:"vram_gb" json:"vram_gb"`
	RamGB       int               `yaml:"ram_gb" json:"ram_gb"`
	DiskGB      int               `yaml:"disk_gb" json:"disk_gb"`
	Labels      map[string]string `yaml:"labels" json:"labels"`
}

type HostInfo struct {
	Hostname    string
	Address     string
	SSHUser     string
	Arch        string
	OS          string
	Accelerator string
	VramGB      int
	RamGB       int
	DiskGB      int
}

func main() {
	hostsFile, hostList, outputFile, keyPath, knownHostsPath, timeout, jsonOutput, insecureSkipHostKeyCheck := parseFlags()

	hosts := getHosts(hostsFile, hostList)
	if len(hosts) == 0 {
		log.Fatal("No hosts provided")
	}

	signer, err := loadSSHKey(keyPath)
	if err != nil {
		log.Fatalf("Failed to load SSH key: %v", err)
	}

	nodes := probeHosts(hosts, signer, knownHostsPath, timeout, insecureSkipHostKeyCheck)

	outputResults(nodes, jsonOutput, outputFile)
}

func parseFlags() (hostsFile, hostList, outputFile, keyPath, knownHostsPath string, timeout int, jsonOutput, insecureSkipHostKeyCheck bool) {
	hostsFileFlag := flag.String("hosts", "", "File containing hosts (one per line: user@host:port or user@host)")
	hostListFlag := flag.String("host", "", "Single host to probe (format: user@host:port)")
	outputFileFlag := flag.String("output", "", "Output inventory YAML file")
	keyPathFlag := flag.String("key", "", "SSH private key path (default: ~/.ssh/id_rsa)")
	knownHostsPathFlag := flag.String("known-hosts", "", "SSH known_hosts file (default: ~/.ssh/known_hosts)")
	insecureSkipHostKeyCheckFlag := flag.Bool("insecure-skip-hostkey-check", false, "Skip SSH host key verification")
	timeoutFlag := flag.Int("timeout", 10, "SSH timeout in seconds")
	jsonOutputFlag := flag.Bool("json", false, "Output JSON instead of YAML")
	flag.Parse()

	return *hostsFileFlag, *hostListFlag, *outputFileFlag, *keyPathFlag, *knownHostsPathFlag, *timeoutFlag, *jsonOutputFlag, *insecureSkipHostKeyCheckFlag
}

func getHosts(hostsFile, hostList string) []string {
	if hostList != "" {
		return []string{hostList}
	}

	if hostsFile == "" {
		log.Fatal("Must specify either -hosts or -host")
	}

	hosts, err := readHostsFile(hostsFile)
	if err != nil {
		log.Fatalf("Failed to read hosts file: %v", err)
	}
	return hosts
}

func probeHosts(hosts []string, signer ssh.Signer, knownHostsPath string, timeout int, insecureSkipHostKeyCheck bool) []*Node {
	var nodes []*Node
	for _, hostStr := range hosts {
		info, err := probeHost(hostStr, signer, knownHostsPath, timeout, insecureSkipHostKeyCheck)
		if err != nil {
			log.Printf("Failed to probe %s: %v", hostStr, err)
			continue
		}

		node := &Node{
			Hostname:    info.Hostname,
			SSHUser:     info.SSHUser,
			Address:     info.Address,
			Arch:        info.Arch,
			OS:          info.OS,
			Role:        "worker",
			Accelerator: info.Accelerator,
			VramGB:      info.VramGB,
			RamGB:       info.RamGB,
			DiskGB:      info.DiskGB,
			Labels:      make(map[string]string),
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func outputResults(nodes []*Node, jsonOutput bool, outputFile string) {
	if jsonOutput {
		outputJSON(nodes, outputFile)
	} else {
		outputYAML(nodes, outputFile)
	}
}

func readHostsFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var hosts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			hosts = append(hosts, line)
		}
	}
	return hosts, scanner.Err()
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
		// Try agent if key file doesn't exist
		return getAgentSigner()
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		// Fallback to agent
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

func probeHost(hostStr string, signer ssh.Signer, knownHostsPath string, timeoutSec int, insecureSkipHostKeyCheck bool) (*HostInfo, error) {
	// Parse host string
	user, address, port := parseHostString(hostStr)
	if user == "" {
		user = "root"
	}
	if port == "" {
		port = "22"
	}

	// Connect to host
	client, err := createSSHClient(user, address, port, signer, knownHostsPath, timeoutSec, insecureSkipHostKeyCheck)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Gather hardware info
	hostname, err := remoteCmd(client, "hostname -f")
	if err != nil {
		hostname = address
	}
	hostname = strings.TrimSpace(hostname)

	arch, _ := remoteCmd(client, "uname -m")
	arch = normalizeArch(strings.TrimSpace(arch))

	osName, _ := remoteCmd(client, "uname -s")
	osName = strings.TrimSpace(osName)
	osName = strings.ToLower(osName)

	// Detect accelerator
	accelerator, vram := detectAccelerator(client)

	// Get RAM in GB
	ramGB := getRamGB(client)

	// Get disk in GB
	diskGB := getDiskGB(client)

	return &HostInfo{
		Hostname:    hostname,
		Address:     address,
		SSHUser:     user,
		Arch:        arch,
		OS:          osName,
		Accelerator: accelerator,
		VramGB:      vram,
		RamGB:       ramGB,
		DiskGB:      diskGB,
	}, nil
}

func parseHostString(hostStr string) (user, address, port string) {
	// Format: [user@]address[:port]
	if strings.Contains(hostStr, "@") {
		parts := strings.SplitN(hostStr, "@", 2)
		user = parts[0]
		hostStr = parts[1]
	}

	if strings.Contains(hostStr, ":") {
		parts := strings.SplitN(hostStr, ":", 2)
		address = parts[0]
		port = parts[1]
	} else {
		address = hostStr
	}

	return user, address, port
}

func createSSHClient(user, address, port string, signer ssh.Signer, knownHostsPath string, timeoutSec int, insecureSkipHostKeyCheck bool) (*ssh.Client, error) {
	hostKeyCallback, err := sshutil.HostKeyCallback(knownHostsPath, insecureSkipHostKeyCheck)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(timeoutSec) * time.Second,
	}

	return ssh.Dial("tcp", address+":"+port, config)
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

func remoteCmd(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	if err := session.Run(cmd); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func detectAccelerator(client *ssh.Client) (string, int) {
	// Check for NVIDIA GPU
	if out, err := remoteCmd(client, "which nvidia-smi"); err == nil && out != "" {
		return detectNvidiaGPU(client)
	}

	// Check for AMD GPU
	if out, err := remoteCmd(client, "which rocm-smi"); err == nil && out != "" {
		return detectAMDGPU(client)
	}

	// Check for Apple Silicon
	if out, err := remoteCmd(client, "system_profiler SPDisplaysDataType 2>/dev/null | grep -i gpu"); err == nil && out != "" {
		return "metal", getAppleSiliconVRAM(client)
	}

	return "cpu", 0
}

func detectNvidiaGPU(client *ssh.Client) (string, int) {
	// Query total VRAM using nvidia-smi
	out, err := remoteCmd(client, "nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits | head -1")
	if err != nil || out == "" {
		return "cuda", 0
	}

	vram := 0
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) > 0 {
		if mb, err := strconv.Atoi(fields[0]); err == nil {
			vram = mb / 1024 // Convert MB to GB
		}
	}
	return "cuda", vram
}

func detectAMDGPU(client *ssh.Client) (string, int) {
	// Query total VRAM using rocm-smi
	out, err := remoteCmd(client, "rocm-smi --showmeminfo 2>/dev/null | grep -i 'total' | head -1")
	if err != nil || out == "" {
		return "rocm", 0
	}

	vram := 0
	fields := strings.Fields(strings.TrimSpace(out))
	for _, field := range fields {
		if mb, err := strconv.Atoi(strings.TrimSuffix(field, "MB")); err == nil {
			vram = mb / 1024 // Convert MB to GB
			break
		}
	}
	return "rocm", vram
}

func getAppleSiliconVRAM(client *ssh.Client) int {
	out, err := remoteCmd(client, "sysctl -n hw.memsize")
	if err != nil || out == "" {
		return 0
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0
	}
	return int(bytes / (1024 * 1024 * 1024))
}

func getRamGB(client *ssh.Client) int {
	// Try Linux first
	out, err := remoteCmd(client, "grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}'")
	if err == nil && out != "" {
		if kb, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return kb / (1024 * 1024)
		}
	}

	// Try macOS
	out, err = remoteCmd(client, "sysctl -n hw.memsize")
	if err == nil && out != "" {
		if bytes, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
			return int(bytes / (1024 * 1024 * 1024))
		}
	}

	return 0
}

func getDiskGB(client *ssh.Client) int {
	// Try getting root filesystem size
	out, err := remoteCmd(client, "df / 2>/dev/null | tail -1 | awk '{print $2}'")
	if err == nil && out != "" {
		if kb, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return kb / (1024 * 1024)
		}
	}

	return 0
}

func outputYAML(nodes []*Node, outputPath string) {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.WriteString("nodes:\n")

	for _, node := range nodes {
		fmt.Fprintf(&buf, "  - hostname: %s\n", node.Hostname)
		fmt.Fprintf(&buf, "    ssh_user: %s\n", node.SSHUser)
		fmt.Fprintf(&buf, "    address: %s\n", node.Address)
		fmt.Fprintf(&buf, "    arch: %s\n", node.Arch)
		fmt.Fprintf(&buf, "    os: %s\n", node.OS)
		fmt.Fprintf(&buf, "    role: %s\n", node.Role)
		fmt.Fprintf(&buf, "    accelerator: %s\n", node.Accelerator)
		fmt.Fprintf(&buf, "    vram_gb: %d\n", node.VramGB)
		fmt.Fprintf(&buf, "    ram_gb: %d\n", node.RamGB)
		fmt.Fprintf(&buf, "    disk_gb: %d\n", node.DiskGB)

		if len(node.Labels) > 0 {
			buf.WriteString("    labels:\n")
			for k, v := range node.Labels {
				fmt.Fprintf(&buf, "      %s: %s\n", k, v)
			}
		}
		buf.WriteString("\n")
	}

	output := buf.String()
	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}
		fmt.Printf("Inventory written to %s\n", outputPath)
	} else {
		fmt.Print(output)
	}
}

func outputJSON(nodes []*Node, outputPath string) {
	data := map[string]interface{}{
		"nodes": nodes,
	}

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, jsonBytes, 0o644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}
		fmt.Printf("Inventory written to %s\n", outputPath)
	} else {
		fmt.Println(string(jsonBytes))
	}
}
