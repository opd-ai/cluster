// cmd/placer implements the model placement policy for the AI cluster.
//
// Placer reads the cluster inventory and Ollama /api/tags from each node,
// then decides which node(s) should serve a given model based on:
//
//  1. Available VRAM (from the "vram" node label — GiB as integer).
//  2. Recent access patterns (tracked in a local LRU state file).
//  3. GPU architecture preference (from the "arch" node label).
//
// Multi-device inference (llama.cpp RPC) is available behind the
// --multi-device feature flag.  When enabled, placer emits a split plan
// across up to N nodes of the same architecture tier that together satisfy
// the model's VRAM requirement.
//
// Usage:
//
//	placer [flags] <model>
//
// Flags:
//
//	-inventory      path to inventory YAML (default: cluster/inventory.yaml)
//	-state-file     path to LRU state JSON (default: /var/lib/aicluster/placer-state.json)
//	-multi-device   enable multi-device inference split (llama.cpp RPC)
//	-max-devices    maximum nodes to split across (default: 4)
//	-format         output format: text|json (default: text)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Node holds placement-relevant metadata for a single cluster node.
type Node struct {
	Name    string
	Address string
	VRAM    int    // GiB, 0 = CPU-only
	Arch    string // e.g. "cuda", "metal", "rocm", ""
	Role    string // "worker", "control"

	// Populated at runtime by probeNode.
	Healthy      bool
	LoadedModels []string
	FreeVRAM     int // estimated free VRAM GiB
}

// PlacementPlan describes where and how to run a model.
type PlacementPlan struct {
	Model   string    `json:"model"`
	Nodes   []string  `json:"nodes"`
	Multi   bool      `json:"multi_device"`
	Reason  string    `json:"reason"`
	Created time.Time `json:"created_at"`
}

// placerState persists access counters across invocations.
type placerState struct {
	AccessCount map[string]int `json:"access_count"` // model → count
}

func main() {
	inventoryPath := flag.String("inventory", "cluster/inventory.yaml", "Inventory YAML")
	stateFile := flag.String("state-file", "/var/lib/aicluster/placer-state.json", "LRU state JSON")
	multiDevice := flag.Bool("multi-device", false, "Enable multi-device inference (llama.cpp RPC)")
	maxDevices := flag.Int("max-devices", 4, "Max nodes for multi-device split")
	format := flag.String("format", "text", "Output format: text|json")
	flag.Parse()

	model := flag.Arg(0)
	if model == "" {
		flag.Usage()
		log.Fatal("model argument required")
	}

	nodes := parseInventory(*inventoryPath)
	if len(nodes) == 0 {
		log.Fatalf("no nodes found in inventory %s", *inventoryPath)
	}

	// Probe all nodes in parallel.
	probeNodes(nodes)

	// Warn if no VRAM-bearing nodes found.
	var vramNodes int
	for _, n := range nodes {
		if n.VRAM > 0 {
			vramNodes++
		}
	}
	if vramNodes == 0 && len(nodes) > 0 {
		log.Printf("warning: no VRAM-bearing nodes found in inventory; GPU placement unavailable")
	}

	state := loadState(*stateFile)
	state.AccessCount[model]++
	if err := saveState(*stateFile, state); err != nil {
		log.Printf("warning: save state: %v", err)
	}

	plan := buildPlan(model, nodes, state, *multiDevice, *maxDevices)

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(plan); err != nil {
			log.Fatalf("json encode: %v", err)
		}
		return
	}

	// Text output.
	fmt.Printf("Model:        %s\n", plan.Model)
	fmt.Printf("Nodes:        %s\n", strings.Join(plan.Nodes, ", "))
	fmt.Printf("Multi-device: %v\n", plan.Multi)
	fmt.Printf("Reason:       %s\n", plan.Reason)
}

// -------------------------------------------------------------------------
// Placement logic
// -------------------------------------------------------------------------

// buildPlan selects the optimal node(s) for the model.
func buildPlan(model string, nodes []*Node, state *placerState, multi bool, maxDevices int) PlacementPlan {
	plan := PlacementPlan{Model: model, Created: time.Now()}

	// Prefer a node that already has the model loaded (warm cache).
	for _, n := range nodes {
		if n.Healthy && containsModel(n.LoadedModels, model) {
			plan.Nodes = []string{n.Name}
			plan.Reason = "model already loaded on node"
			return plan
		}
	}

	// Sort healthy GPU nodes by free VRAM descending.
	var gpu []*Node
	for _, n := range nodes {
		if n.Healthy && n.VRAM > 0 {
			gpu = append(gpu, n)
		}
	}
	sort.Slice(gpu, func(i, j int) bool {
		return gpu[i].FreeVRAM > gpu[j].FreeVRAM
	})

	if len(gpu) == 0 {
		// Fall back to any healthy CPU node.
		for _, n := range nodes {
			if n.Healthy {
				plan.Nodes = []string{n.Name}
				plan.Reason = "no GPU nodes available; using CPU fallback"
				return plan
			}
		}
		plan.Reason = "no healthy nodes available"
		return plan
	}

	if !multi || len(gpu) == 1 {
		plan.Nodes = []string{gpu[0].Name}
		plan.Reason = fmt.Sprintf("highest free VRAM (%d GiB)", gpu[0].FreeVRAM)
		return plan
	}

	// Multi-device: pick up to maxDevices nodes of the same arch tier.
	arch := gpu[0].Arch
	var chosen []*Node
	for _, n := range gpu {
		if len(chosen) >= maxDevices {
			break
		}
		if n.Arch == arch {
			chosen = append(chosen, n)
		}
	}

	names := make([]string, len(chosen))
	for i, n := range chosen {
		names[i] = n.Name
	}
	plan.Nodes = names
	plan.Multi = true
	plan.Reason = fmt.Sprintf("multi-device split across %d %s nodes", len(chosen), arch)
	return plan
}

func containsModel(models []string, model string) bool {
	for _, m := range models {
		if m == model || strings.HasPrefix(m, model) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------
// Node probing
// -------------------------------------------------------------------------

func probeNodes(nodes []*Node) {
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 5 * time.Second}
	for _, n := range nodes {
		wg.Add(1)
		go func(node *Node) {
			defer wg.Done()
			probeNode(client, node)
		}(n)
	}
	wg.Wait()
}

func probeNode(client *http.Client, node *Node) {
	url := "http://" + node.Address + ":11434/api/tags"
	resp, err := client.Get(url)
	if err != nil {
		node.Healthy = false
		return
	}
	defer resp.Body.Close()

	node.Healthy = resp.StatusCode == http.StatusOK

	var result struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"` // bytes on disk; approximate VRAM proxy
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("probe %s: decode error: %v", node.Address, err)
		node.Healthy = false
		return
	}
	const bytesPerGiB = 1 << 30
	usedBytes := int64(0)
	for _, m := range result.Models {
		node.LoadedModels = append(node.LoadedModels, m.Name)
		if m.Size > 0 {
			usedBytes += m.Size
		} else {
			// Fallback: assume 8 GiB when size is not reported.
			usedBytes += 8 * bytesPerGiB
		}
	}
	// Convert bytes to GiB, rounding up.
	used := int((usedBytes + bytesPerGiB - 1) / bytesPerGiB)
	if used > node.VRAM {
		used = node.VRAM
	}
	node.FreeVRAM = node.VRAM - used
}

// -------------------------------------------------------------------------
// Inventory parsing
// -------------------------------------------------------------------------

func parseInventory(path string) []*Node {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("inventory: %v", err)
		return nil
	}

	var nodes []*Node
	var current *Node
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "- hostname:") || strings.HasPrefix(trim, "hostname:") {
			current = &Node{}
			name := strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
			current.Name = strings.Trim(name, `"'`)
			if current.Name == "" {
				current = nil
				continue
			}
			nodes = append(nodes, current)
			continue
		}
		if current == nil {
			continue
		}
		kv := parseKV(trim)
		switch kv[0] {
		case "address":
			current.Address = kv[1]
		case "vram_gb":
			v, err := strconv.Atoi(kv[1])
			if err != nil {
				log.Printf("warning: node %q has invalid vram_gb value %q: %v; defaulting to 0", current.Name, kv[1], err)
			}
			current.VRAM = v
			current.FreeVRAM = v
		case "arch":
			current.Arch = kv[1]
		case "role":
			current.Role = kv[1]
		}
	}
	return nodes
}

// parseKV splits "key: value" into [key, value]; returns ["", ""] on failure.
func parseKV(line string) [2]string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return [2]string{"", ""}
	}
	return [2]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
}

// -------------------------------------------------------------------------
// State persistence
// -------------------------------------------------------------------------

func loadState(path string) *placerState {
	s := &placerState{AccessCount: make(map[string]int)}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("warning: unmarshal state: %v", err)
	}
	if s.AccessCount == nil {
		s.AccessCount = make(map[string]int)
	}
	return s
}

func saveState(path string, s *placerState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
