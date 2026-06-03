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
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// LabelConfig holds CLI flags.
type LabelConfig struct {
	InventoryPath  string
	KubeconfigPath string
	DryRun         bool
}

// InventoryNode holds the fields needed to compute labels and taints.
type InventoryNode struct {
	Hostname    string            `yaml:"hostname"`
	Arch        string            `yaml:"arch"`
	OS          string            `yaml:"os"`
	Role        string            `yaml:"role"`
	Accelerator string            `yaml:"accelerator"`
	VramGB      string            `yaml:"vram_gb"`
	Labels      map[string]string `yaml:"labels"`
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
	args := []string{"label", "--overwrite", "node", "--", node.Hostname}
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
		args := []string{"taint", "--overwrite", "node", "--", node.Hostname, taint}
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
