// cmd/cluster-label reads cluster/inventory.yaml and applies Kubernetes node
// labels and taints that match the inventory fields.
//
// Labels applied:
//
//	accelerator=cuda|metal|cpu|rocm
//	vram=<vram_gb>
//	role=trainer|server|imagegen|videogen|both    (from inventory labels.workload)
//	arch=amd64|arm64
//
// Taints applied:
//
//	workload=trainer:NoSchedule   (on nodes with labels.workload=trainer)
//
// This command requires kubectl to be configured (KUBECONFIG env or
// cluster/kubeconfig) and the k3s control-plane to be reachable.
//
// Usage:
//
//	cluster-label [flags]
//
// Flags:
//
//	-inventory   path to inventory YAML (default: cluster/inventory.yaml)
//	-kubeconfig  path to kubeconfig (default: cluster/kubeconfig)
//	-dry-run     print kubectl commands without running them
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// LabelConfig holds CLI flags.
type LabelConfig struct {
	InventoryPath string
	KubeconfigPath string
	DryRun        bool
}

// InventoryNode holds the fields needed to compute labels and taints.
type InventoryNode struct {
	Hostname    string
	Arch        string
	OS          string
	Role        string
	Accelerator string
	VramGB      string
	Labels      map[string]string
}

func main() {
	cfg := LabelConfig{}
	flag.StringVar(&cfg.InventoryPath, "inventory", "cluster/inventory.yaml", "Path to inventory YAML")
	flag.StringVar(&cfg.KubeconfigPath, "kubeconfig", "cluster/kubeconfig", "Path to kubeconfig")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Print kubectl commands without running them")
	flag.Parse()

	nodes, err := loadInventory(cfg.InventoryPath)
	if err != nil {
		log.Fatalf("load inventory: %v", err)
	}

	for _, node := range nodes {
		if node.OS == "darwin" {
			fmt.Printf("skip %s (darwin — not a k3s node)\n", node.Hostname)
			continue
		}
		if err := applyLabels(node, cfg); err != nil {
			log.Printf("label %s: %v", node.Hostname, err)
		}
		if err := applyTaints(node, cfg); err != nil {
			log.Printf("taint %s: %v", node.Hostname, err)
		}
	}
}

func applyLabels(node InventoryNode, cfg LabelConfig) error {
	labels := buildLabels(node)
	if len(labels) == 0 {
		return nil
	}
	args := []string{"label", "--overwrite", "node", node.Hostname}
	args = append(args, labels...)
	return kubectl(args, cfg)
}

func buildLabels(node InventoryNode) []string {
	var labels []string

	if node.Accelerator != "" {
		labels = append(labels, "accelerator="+node.Accelerator)
	}
	if node.VramGB != "" && node.VramGB != "0" {
		labels = append(labels, "vram="+node.VramGB)
	}
	if node.Arch != "" {
		labels = append(labels, "arch="+node.Arch)
	}

	workload := strings.ToLower(strings.TrimSpace(node.Labels["workload"]))
	if workload != "" {
		labels = append(labels, "role="+workload)
	}

	return labels
}

func applyTaints(node InventoryNode, cfg LabelConfig) error {
	taints := buildTaints(node)
	for _, taint := range taints {
		args := []string{"taint", "--overwrite", "node", node.Hostname, taint}
		if err := kubectl(args, cfg); err != nil {
			return err
		}
	}
	return nil
}

func buildTaints(node InventoryNode) []string {
	workload := strings.ToLower(strings.TrimSpace(node.Labels["workload"]))
	if workload == "trainer" {
		return []string{"workload=trainer:NoSchedule"}
	}
	return nil
}

func kubectl(args []string, cfg LabelConfig) error {
	allArgs := append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
	if cfg.DryRun {
		fmt.Printf("[DRY-RUN] kubectl %s\n", strings.Join(allArgs, " "))
		return nil
	}
	fmt.Printf("kubectl %s\n", strings.Join(allArgs, " "))
	cmd := exec.Command("kubectl", allArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func loadInventory(path string) ([]InventoryNode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var nodes []InventoryNode
	var cur InventoryNode
	inLabels := false
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "nodes:" {
			continue
		}

		if strings.HasPrefix(trimmed, "- hostname:") {
			if cur.Hostname != "" {
				nodes = append(nodes, cur)
			}
			cur = InventoryNode{Labels: make(map[string]string)}
			cur.Hostname = strings.TrimSpace(strings.TrimPrefix(trimmed, "- hostname:"))
			inLabels = false
			continue
		}

		if trimmed == "labels:" {
			inLabels = true
			continue
		}

		if inLabels {
			if indent >= 6 {
				k, v, ok := strings.Cut(trimmed, ":")
				if ok {
					cur.Labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
				continue
			}
			inLabels = false
		}

		parseField(&cur, trimmed)
	}

	if cur.Hostname != "" {
		nodes = append(nodes, cur)
	}
	return nodes, scanner.Err()
}

func parseField(n *InventoryNode, line string) {
	switch {
	case strings.HasPrefix(line, "arch:"):
		n.Arch = strings.TrimSpace(strings.TrimPrefix(line, "arch:"))
	case strings.HasPrefix(line, "os:"):
		n.OS = strings.TrimSpace(strings.TrimPrefix(line, "os:"))
	case strings.HasPrefix(line, "role:"):
		n.Role = strings.TrimSpace(strings.TrimPrefix(line, "role:"))
	case strings.HasPrefix(line, "accelerator:"):
		n.Accelerator = strings.TrimSpace(strings.TrimPrefix(line, "accelerator:"))
	case strings.HasPrefix(line, "vram_gb:"):
		n.VramGB = strings.TrimSpace(strings.TrimPrefix(line, "vram_gb:"))
	}
}
