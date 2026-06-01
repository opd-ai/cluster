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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/opd-ai/cluster/internal/sshutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/sys/unix"
)

type DoctorConfig struct {
	Host                     string
	User                     string
	KeyPath                  string
	KnownHostsPath           string
	InsecureSkipHostKeyCheck bool
	DiskThreshold            int
	CheckRemote              bool
}

type HealthReport struct {
	Hostname  string
	Checks    []CheckResult
	AllPassed bool
}

type CheckResult struct {
	Name    string
	Status  string // "PASS", "WARN", "FAIL"
	Message string
}

func main() {
	config := DoctorConfig{}
	flag.StringVar(&config.Host, "host", "", "Remote host to check (requires -remote)")
	flag.StringVar(&config.User, "user", "root", "SSH user for remote host")
	flag.StringVar(&config.KeyPath, "key", "", "SSH private key path")
	flag.StringVar(&config.KnownHostsPath, "known-hosts", "", "SSH known_hosts file (default: ~/.ssh/known_hosts)")
	flag.BoolVar(&config.InsecureSkipHostKeyCheck, "insecure-skip-hostkey-check", false, "Skip SSH host key verification")
	flag.IntVar(&config.DiskThreshold, "disk-threshold", 50, "Minimum free disk space in GB")
	flag.BoolVar(&config.CheckRemote, "remote", false, "Enable remote checks")
	flag.Parse()

	var report HealthReport
	report.AllPassed = true

	// Get hostname
	if config.CheckRemote {
		if config.Host == "" {
			log.Fatal("-host is required when -remote is set")
		}
		report.Hostname = config.Host
		signer, err := loadSSHKey(config.KeyPath)
		if err != nil {
			log.Fatalf("Failed to load SSH key: %v", err)
		}
		report = performRemoteChecks(config.Host, config.User, signer, config)
	} else {
		if config.Host != "" {
			log.Fatal("use -remote with -host to run remote checks")
		}
		host, _ := os.Hostname()
		report.Hostname = host
		report = performLocalChecks(config)
	}

	// Print results
	printReport(report)

	// Exit with appropriate code
	if !report.AllPassed {
		os.Exit(1)
	}
}

func performLocalChecks(config DoctorConfig) HealthReport {
	var report HealthReport
	report.AllPassed = true

	// Get hostname
	hostname, _ := os.Hostname()
	report.Hostname = hostname

	// Check 1: GPU visibility
	report.Checks = append(report.Checks, checkGPU())

	// Check 2: FP16/BF16 support
	report.Checks = append(report.Checks, checkFPSupport())

	// Check 3: Disk space
	report.Checks = append(report.Checks, checkDiskSpace(config.DiskThreshold))

	// Check 4: Clock skew
	report.Checks = append(report.Checks, checkClockSkew())

	// Check 5: MTU
	report.Checks = append(report.Checks, checkMTU())

	// Check 6: HTTPS connectivity
	report.Checks = append(report.Checks, checkHTTPSConnectivity())

	// Check if all passed
	for _, check := range report.Checks {
		if check.Status != "PASS" {
			report.AllPassed = false
		}
	}

	return report
}

func performRemoteChecks(host, user string, signer ssh.Signer, config DoctorConfig) HealthReport {
	var report HealthReport
	report.Hostname = host

	client, err := createSSHClient(user, host, "22", signer, config)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "SSH Connectivity",
			Status:  "FAIL",
			Message: fmt.Sprintf("Failed to connect: %v", err),
		})
		report.AllPassed = false
		return report
	}
	defer client.Close()

	// Run remote checks
	report.Checks = append(report.Checks, checkRemoteGPU(client))
	report.Checks = append(report.Checks, checkRemoteFPSupport(client))
	report.Checks = append(report.Checks, checkRemoteDiskSpace(client, config.DiskThreshold))
	report.Checks = append(report.Checks, checkRemoteClockSkew(client))
	report.Checks = append(report.Checks, checkRemoteMTU(client))
	report.Checks = append(report.Checks, checkRemoteHTTPSConnectivity(client))

	// Check if all passed
	for _, check := range report.Checks {
		if check.Status != "PASS" {
			report.AllPassed = false
		}
	}

	return report
}

func loadSSHKey(keyPath string) (ssh.Signer, error) {
	if keyPath == "" {
		usr, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		keyPath = filepath.Join(usr.HomeDir, ".ssh", "id_rsa")
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
		return nil, fmt.Errorf("no SSH agent: %v", err)
	}

	ag := agent.NewClient(agentConn)
	signers, err := ag.Signers()
	if err != nil {
		return nil, err
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no signers in agent")
	}
	return signers[0], nil
}

func createSSHClient(user, address, port string, signer ssh.Signer, doctorConfig DoctorConfig) (*ssh.Client, error) {
	hostKeyCallback, err := sshutil.HostKeyCallback(doctorConfig.KnownHostsPath, doctorConfig.InsecureSkipHostKeyCheck)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
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

// ============================================================================
// Local checks
// ============================================================================

func checkGPU() CheckResult {
	// Check for NVIDIA GPU
	if out, err := runCmd("which", "nvidia-smi"); err == nil && out != "" {
		if out, err := runCmd("nvidia-smi", "-L"); err == nil {
			return CheckResult{
				Name:    "GPU Visibility",
				Status:  "PASS",
				Message: fmt.Sprintf("NVIDIA GPU(s) detected: %s", strings.TrimSpace(out)),
			}
		}
	}

	// Check for AMD GPU
	if out, err := runCmd("which", "rocm-smi"); err == nil && out != "" {
		return CheckResult{
			Name:    "GPU Visibility",
			Status:  "PASS",
			Message: "AMD GPU detected",
		}
	}

	// Check for Apple Metal
	if runtime.GOOS == "darwin" {
		return CheckResult{
			Name:    "GPU Visibility",
			Status:  "PASS",
			Message: "Apple Metal (macOS)",
		}
	}

	return CheckResult{
		Name:    "GPU Visibility",
		Status:  "WARN",
		Message: "No GPU detected (CPU only)",
	}
}

func checkFPSupport() CheckResult {
	// Try to detect FP16/BF16 support via GPU
	if out, err := runCmd("nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader"); err == nil {
		if supported, capabilities, ok := computeCapabilitySupport(out); ok && supported {
			return CheckResult{
				Name:    "FP16/BF16 Support",
				Status:  "PASS",
				Message: fmt.Sprintf("Compute capability: %s (FP16/BF16 supported)", capabilities),
			}
		} else if ok {
			return CheckResult{
				Name:    "FP16/BF16 Support",
				Status:  "FAIL",
				Message: fmt.Sprintf("Compute capability: %s (need ≥ 7.0)", capabilities),
			}
		}
	}

	return CheckResult{
		Name:    "FP16/BF16 Support",
		Status:  "WARN",
		Message: "Unable to verify FP16/BF16 support",
	}
}

func checkDiskSpace(threshold int) CheckResult {
	const path = "/"
	free := getFreeDiskGB(path)

	if free < 0 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "WARN",
			Message: "Unable to determine free disk space",
		}
	}

	if free < threshold {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "FAIL",
			Message: fmt.Sprintf("Free space: %dGB (need ≥%dGB)", free, threshold),
		}
	}

	return CheckResult{
		Name:    "Disk Space",
		Status:  "PASS",
		Message: fmt.Sprintf("Free space: %dGB", free),
	}
}

func checkClockSkew() CheckResult {
	// Check NTP sync status
	if out, err := runCmd("timedatectl", "status"); err == nil && strings.Contains(out, "synchronized: yes") {
		return CheckResult{
			Name:    "Clock Skew (NTP)",
			Status:  "PASS",
			Message: "NTP synchronized",
		}
	}

	return CheckResult{
		Name:    "Clock Skew (NTP)",
		Status:  "WARN",
		Message: "Unable to verify NTP synchronization",
	}
}

func checkMTU() CheckResult {
	// Check default MTU
	out, err := runCmd("ip", "link", "show")
	if err != nil || !strings.Contains(out, "mtu") {
		return CheckResult{
			Name:    "MTU",
			Status:  "WARN",
			Message: "Unable to verify MTU",
		}
	}

	mtu, found := extractMTUValue(out)
	if !found {
		return CheckResult{
			Name:    "MTU",
			Status:  "WARN",
			Message: "Unable to parse MTU value",
		}
	}

	if mtu >= 1500 {
		return CheckResult{
			Name:    "MTU",
			Status:  "PASS",
			Message: fmt.Sprintf("MTU: %d", mtu),
		}
	}

	return CheckResult{
		Name:    "MTU",
		Status:  "WARN",
		Message: fmt.Sprintf("MTU: %d (< 1500)", mtu),
	}
}

func extractMTUValue(output string) (int, bool) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "mtu") {
			continue
		}

		parts := strings.Fields(line)
		for i, part := range parts {
			if part == "mtu" && i+1 < len(parts) {
				mtu, err := strconv.Atoi(parts[i+1])
				if err == nil {
					return mtu, true
				}
			}
		}
	}
	return 0, false
}

func checkHTTPSConnectivity() CheckResult {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://www.google.com")
	if err == nil {
		resp.Body.Close()
		return CheckResult{
			Name:    "HTTPS Connectivity",
			Status:  "PASS",
			Message: "Outbound HTTPS working",
		}
	}

	return CheckResult{
		Name:    "HTTPS Connectivity",
		Status:  "WARN",
		Message: fmt.Sprintf("HTTPS check failed: %v", err),
	}
}

// ============================================================================
// Remote checks
// ============================================================================

func checkRemoteGPU(client *ssh.Client) CheckResult {
	if out, err := remoteCmd(client, "which nvidia-smi 2>/dev/null && nvidia-smi -L"); err == nil && out != "" {
		return CheckResult{
			Name:    "GPU Visibility",
			Status:  "PASS",
			Message: fmt.Sprintf("NVIDIA GPU detected"),
		}
	}

	if out, err := remoteCmd(client, "which rocm-smi 2>/dev/null"); err == nil && out != "" {
		return CheckResult{
			Name:    "GPU Visibility",
			Status:  "PASS",
			Message: "AMD GPU detected",
		}
	}

	return CheckResult{
		Name:    "GPU Visibility",
		Status:  "WARN",
		Message: "No GPU detected",
	}
}

func checkRemoteFPSupport(client *ssh.Client) CheckResult {
	if out, err := remoteCmd(client, "nvidia-smi --query-gpu=compute_cap --format=csv,noheader 2>/dev/null"); err == nil && out != "" {
		if supported, capabilities, ok := computeCapabilitySupport(out); ok && supported {
			return CheckResult{
				Name:    "FP16/BF16 Support",
				Status:  "PASS",
				Message: fmt.Sprintf("Compute capability: %s (FP16/BF16 supported)", capabilities),
			}
		} else if ok {
			return CheckResult{
				Name:    "FP16/BF16 Support",
				Status:  "FAIL",
				Message: fmt.Sprintf("Compute capability: %s (need ≥ 7.0)", capabilities),
			}
		}
	}

	return CheckResult{
		Name:    "FP16/BF16 Support",
		Status:  "WARN",
		Message: "Unable to verify",
	}
}

func checkRemoteDiskSpace(client *ssh.Client, threshold int) CheckResult {
	out, err := remoteCmd(client, "df / | tail -1 | awk '{print $4}'")
	if err == nil && out != "" {
		kb, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		free := int(kb / (1024 * 1024))
		if free < threshold {
			return CheckResult{
				Name:    "Disk Space",
				Status:  "FAIL",
				Message: fmt.Sprintf("Free: %dGB (need ≥%dGB)", free, threshold),
			}
		}
		return CheckResult{
			Name:    "Disk Space",
			Status:  "PASS",
			Message: fmt.Sprintf("Free: %dGB", free),
		}
	}

	return CheckResult{
		Name:    "Disk Space",
		Status:  "WARN",
		Message: "Unable to check",
	}
}

func checkRemoteClockSkew(client *ssh.Client) CheckResult {
	out, err := remoteCmd(client, "timedatectl status 2>/dev/null | grep synchronized")
	if err == nil && strings.Contains(out, "yes") {
		return CheckResult{
			Name:    "Clock Skew (NTP)",
			Status:  "PASS",
			Message: "NTP synchronized",
		}
	}

	return CheckResult{
		Name:    "Clock Skew (NTP)",
		Status:  "WARN",
		Message: "Unable to verify NTP",
	}
}

func checkRemoteMTU(client *ssh.Client) CheckResult {
	out, err := remoteCmd(client, "ip link show | grep -oP '(?<=mtu )\\d+' | head -1")
	if err == nil && out != "" {
		mtu := strings.TrimSpace(out)
		mtuVal, _ := strconv.Atoi(mtu)
		if mtuVal >= 1500 {
			return CheckResult{
				Name:    "MTU",
				Status:  "PASS",
				Message: fmt.Sprintf("MTU: %s", mtu),
			}
		}
	}

	return CheckResult{
		Name:    "MTU",
		Status:  "WARN",
		Message: "Unable to verify MTU",
	}
}

func checkRemoteHTTPSConnectivity(client *ssh.Client) CheckResult {
	_, err := remoteCmd(client, "curl -s --connect-timeout 5 https://www.google.com > /dev/null 2>&1")
	if err == nil {
		return CheckResult{
			Name:    "HTTPS Connectivity",
			Status:  "PASS",
			Message: "Outbound HTTPS working",
		}
	}

	return CheckResult{
		Name:    "HTTPS Connectivity",
		Status:  "WARN",
		Message: "HTTPS check inconclusive",
	}
}

// ============================================================================
// Helpers
// ============================================================================

func runCmd(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	return string(output), err
}

func getFreeDiskGB(path string) int {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return -1
	}

	return int((stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024 * 1024))
}

func computeCapabilitySupport(output string) (bool, string, bool) {
	var capabilities []string
	parsed := 0

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		capabilities = append(capabilities, line)

		capability, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		parsed++
		if capability < 7.0 {
			return false, strings.Join(capabilities, ", "), true
		}
	}

	if parsed == 0 {
		return false, "", false
	}

	return true, strings.Join(capabilities, ", "), true
}

func printReport(report HealthReport) {
	fmt.Printf("\n╔════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║ Health Report: %s\n", report.Hostname)
	fmt.Printf("╚════════════════════════════════════════════════════════════╝\n\n")

	for _, check := range report.Checks {
		status := "✓"
		color := "\033[32m"
		if check.Status == "WARN" {
			status = "⚠"
			color = "\033[33m"
		} else if check.Status == "FAIL" {
			status = "✗"
			color = "\033[31m"
		}
		reset := "\033[0m"

		fmt.Printf("%s%s %s%s\n", color, status, check.Name, reset)
		fmt.Printf("   %s\n\n", check.Message)
	}

	if report.AllPassed {
		fmt.Printf("\n✓ All checks passed\n")
	} else {
		fmt.Printf("\n⚠ Some checks failed or warned\n")
	}
}
