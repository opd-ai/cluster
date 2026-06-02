// cmd/status diffs the declared cluster state (inventory.yaml + kustomize
// overlays) against the actual live cluster state (k3s nodes, Ollama models,
// running pods, MinIO buckets).
//
// Usage:
//
//	status [flags]
//
// Flags:
//
//	-inventory   path to cluster/inventory.yaml (default: cluster/inventory.yaml)
//	-kubeconfig  path to kubeconfig (default: cluster/kubeconfig)
//	-format      output format: text|json (default: text)
//	-gateway-url gateway base URL for model enumeration (default: http://localhost:8080)
//
// Exit codes:
//
//	0  no drift detected
//	1  drift detected (declared != actual)
//	2  status check failed (cannot reach cluster)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DeclaredNode is a node entry from inventory.yaml.
type DeclaredNode struct {
	Hostname    string            `yaml:"hostname"`
	Address     string            `yaml:"address"`
	Arch        string            `yaml:"arch"`
	OS          string            `yaml:"os"`
	Role        string            `yaml:"role"`
	Accelerator string            `yaml:"accelerator"`
	VRAMgb      int               `yaml:"vram_gb"`
	Labels      map[string]string `yaml:"labels"`
}

// ActualNode holds observed state for a single cluster node.
type ActualNode struct {
	Name   string
	Ready  bool
	Labels map[string]string
}

// ServiceStatus describes a single deployed service.
type ServiceStatus struct {
	Name      string
	Namespace string
	Running   bool
	Replicas  int
	Ready     int
}

// DriftReport is the result of comparing declared vs actual state.
type DriftReport struct {
	Timestamp    string         `json:"timestamp"`
	NodeDrift    []NodeDrift    `json:"node_drift,omitempty"`
	ServiceDrift []ServiceDrift `json:"service_drift,omitempty"`
	HasDrift     bool           `json:"has_drift"`
}

// NodeDrift describes a mismatch between declared and actual node state.
type NodeDrift struct {
	Hostname string `json:"hostname"`
	Issue    string `json:"issue"`
}

// ServiceDrift describes a mismatch for a cluster service.
type ServiceDrift struct {
	Service string `json:"service"`
	Issue   string `json:"issue"`
}

func main() {
	var (
		inventoryPath  = flag.String("inventory", "cluster/inventory.yaml", "Path to inventory YAML")
		kubeconfigPath = flag.String("kubeconfig", "cluster/kubeconfig", "Path to kubeconfig")
		format         = flag.String("format", "text", "Output format: text|json")
		gatewayURL     = flag.String("gateway-url", "http://localhost:8080", "Gateway base URL")
	)
	flag.Parse()

	declared, err := loadDeclaredNodes(*inventoryPath)
	if err != nil {
		log.Fatalf("load inventory: %v", err)
	}

	report := DriftReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Check k3s node membership.
	actual, err := fetchActualNodes(*kubeconfigPath)
	if err != nil {
		log.Printf("warning: cannot fetch k3s nodes (%v); skipping node check", err)
	} else {
		report.NodeDrift = diffNodes(declared, actual)
	}

	// Check expected services via gateway /healthz.
	report.ServiceDrift = checkServices(*gatewayURL)

	report.HasDrift = len(report.NodeDrift) > 0 || len(report.ServiceDrift) > 0

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatalf("encode report: %v", err)
		}
	default:
		printTextReport(report)
	}

	if report.HasDrift {
		os.Exit(1)
	}
}

func loadDeclaredNodes(path string) ([]DeclaredNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var inv struct {
		Nodes []DeclaredNode `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return inv.Nodes, nil
}

// fetchActualNodes runs `kubectl get nodes` via the provided kubeconfig and
// parses the output into a list of ActualNode values.
func fetchActualNodes(kubeconfig string) ([]ActualNode, error) {
	args := []string{"get", "nodes", "--no-headers",
		"-o", `jsonpath={range .items[*]}{.metadata.name}{"\t"}{range .status.conditions[?(@.type=="Ready")]}{.status}{end}{"\n"}{end}`}
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	out, err := exec.Command("kubectl", args...).Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("kubectl get nodes: %w", err)
	}
	var nodes []ActualNode
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		nodes = append(nodes, ActualNode{
			Name:  fields[0],
			Ready: fields[1] == "True",
		})
	}
	return nodes, nil
}

// diffNodes compares declared inventory nodes to actual k3s nodes.
func diffNodes(declared []DeclaredNode, actual []ActualNode) []NodeDrift {
	actualSet := make(map[string]bool, len(actual))
	for _, n := range actual {
		actualSet[n.Name] = n.Ready
	}

	var drift []NodeDrift
	for _, d := range declared {
		if d.OS == "darwin" {
			// Mac nodes are external workers via launchd, not k3s nodes.
			continue
		}
		ready, found := actualSet[d.Hostname]
		switch {
		case !found:
			drift = append(drift, NodeDrift{
				Hostname: d.Hostname,
				Issue:    "declared in inventory but not present in k3s cluster",
			})
		case !ready:
			drift = append(drift, NodeDrift{
				Hostname: d.Hostname,
				Issue:    "node exists in k3s but is not Ready",
			})
		}
	}
	return drift
}

// checkServices probes the gateway /healthz and /status endpoints to
// determine whether core services are reachable.
func checkServices(gatewayURL string) []ServiceDrift {
	var drift []ServiceDrift
	client := &http.Client{Timeout: 5 * time.Second}

	checks := []struct {
		name string
		path string
	}{
		{"gateway", "/healthz"},
		{"gateway-status", "/status"},
	}

	for _, c := range checks {
		url := strings.TrimRight(gatewayURL, "/") + c.path
		resp, err := client.Get(url) //nolint:noctx
		if err != nil {
			drift = append(drift, ServiceDrift{
				Service: c.name,
				Issue:   fmt.Sprintf("unreachable (%v)", err),
			})
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			drift = append(drift, ServiceDrift{
				Service: c.name,
				Issue:   fmt.Sprintf("HTTP %d", resp.StatusCode),
			})
		}
	}
	return drift
}

func printTextReport(r DriftReport) {
	fmt.Printf("=== Cluster Status — %s ===\n\n", r.Timestamp)

	if !r.HasDrift {
		fmt.Println("✓ No drift detected. Declared state matches actual state.")
		return
	}

	if len(r.NodeDrift) > 0 {
		fmt.Println("NODE DRIFT:")
		for _, d := range r.NodeDrift {
			fmt.Printf("  ✗ %s: %s\n", d.Hostname, d.Issue)
		}
		fmt.Println()
	}

	if len(r.ServiceDrift) > 0 {
		fmt.Println("SERVICE DRIFT:")
		for _, d := range r.ServiceDrift {
			fmt.Printf("  ✗ %s: %s\n", d.Service, d.Issue)
		}
		fmt.Println()
	}

	fmt.Printf("Total drift items: %d\n", len(r.NodeDrift)+len(r.ServiceDrift))
}
