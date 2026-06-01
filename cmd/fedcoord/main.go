// cmd/fedcoord implements the federated LoRA training coordinator.
//
// In federated mode each participating node trains a local adapter on its
// own data.  The coordinator runs one round at a time:
//
//  1. Broadcast the current global adapter to all participants.
//  2. Wait for each node to finish local training and upload its adapter.
//  3. Aggregate all adapters using FedAvg (or FedProx with a proximity term).
//  4. Evaluate the merged adapter on the holdout set.
//  5. Repeat for the configured number of rounds.
//
// Adapters are stored in MinIO (or a plain local path for testing) under
// fed/<round>/<node>/adapter.safetensors.
//
// Nodes opt in via the inventory label `federated=true`.
//
// Usage:
//
//	fedcoord [flags]
//
// Flags:
//
//	-inventory      path to inventory YAML (default: cluster/inventory.yaml)
//	-namespaces     path to namespaces.yaml (default: configs/namespaces.yaml)
//	-namespace      namespace to federate (required)
//	-rounds         number of aggregation rounds (default: 5)
//	-local-epochs   local training epochs per round (default: 1)
//	-fed-dir        base directory for per-round adapter exchange (default: /mnt/warm/fed)
//	-algorithm      aggregation algorithm: fedavg|fedprox (default: fedavg)
//	-proxmu         FedProx µ proximity coefficient (default: 0.01)
//	-timeout        per-round timeout in minutes (default: 60)
//	-dry-run        print plan without executing
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FedNode is a cluster node that participates in federated training.
type FedNode struct {
	Name    string
	Address string
	SSHUser string
}

func main() {
	inventoryPath := flag.String("inventory", "cluster/inventory.yaml", "Inventory YAML")
	namespacesPath := flag.String("namespaces", "configs/namespaces.yaml", "Namespaces YAML")
	nsName := flag.String("namespace", "", "Namespace to federate (required)")
	rounds := flag.Int("rounds", 5, "Number of federated rounds")
	localEpochs := flag.Int("local-epochs", 1, "Local training epochs per round")
	fedDir := flag.String("fed-dir", "/mnt/warm/fed", "Base directory for adapter exchange")
	algorithm := flag.String("algorithm", "fedavg", "Aggregation algorithm: fedavg|fedprox")
	proxMu := flag.Float64("proxmu", 0.01, "FedProx µ coefficient")
	timeout := flag.Int("timeout", 60, "Per-round timeout (minutes)")
	dryRun := flag.Bool("dry-run", false, "Print plan without executing")
	flag.Parse()

	if *nsName == "" {
		flag.Usage()
		log.Fatal("-namespace is required")
	}

	nodes := loadFedNodes(*inventoryPath)
	if len(nodes) == 0 {
		log.Fatalf("no federated nodes found in inventory (label federated=true required)")
	}

	log.Printf("fedcoord: namespace=%s rounds=%d nodes=%d algorithm=%s",
		*nsName, *rounds, len(nodes), *algorithm)
	for _, n := range nodes {
		log.Printf("  participant: %s (%s)", n.Name, n.Address)
	}

	globalAdapter := "" // path to current global adapter (empty = base model)

	for round := 1; round <= *rounds; round++ {
		log.Printf("=== Round %d/%d ===", round, *rounds)
		roundDir := filepath.Join(*fedDir, fmt.Sprintf("round-%03d", round))

		if !*dryRun {
			if err := os.MkdirAll(roundDir, 0o755); err != nil {
				log.Fatalf("mkdir %s: %v", roundDir, err)
			}
		}

		// Step 1: Broadcast global adapter to nodes.
		if err := broadcastAdapter(nodes, globalAdapter, roundDir, *dryRun); err != nil {
			log.Fatalf("round %d broadcast: %v", round, err)
		}

		// Step 2: Trigger local training on each node.
		if err := triggerLocalTraining(nodes, *nsName, *namespacesPath, globalAdapter,
			roundDir, round, *localEpochs, *algorithm, *proxMu, *dryRun); err != nil {
			log.Fatalf("round %d local training: %v", round, err)
		}

		// Step 3: Wait for all nodes to upload their adapters.
		deadline := time.Now().Add(time.Duration(*timeout) * time.Minute)
		if err := waitForAdapters(nodes, roundDir, deadline, *dryRun); err != nil {
			log.Fatalf("round %d wait: %v", round, err)
		}

		// Step 4: Aggregate adapters.
		aggregatedPath := filepath.Join(roundDir, "global", "adapter.safetensors")
		if err := aggregateAdapters(nodes, roundDir, aggregatedPath, *algorithm, *proxMu, *dryRun); err != nil {
			log.Fatalf("round %d aggregate: %v", round, err)
		}
		globalAdapter = aggregatedPath

		log.Printf("Round %d complete; global adapter: %s", round, aggregatedPath)
	}

	log.Printf("fedcoord complete after %d rounds; final adapter: %s", *rounds, globalAdapter)
}

// broadcastAdapter distributes the current global adapter to each node's
// round-specific directory.
func broadcastAdapter(nodes []FedNode, adapterPath, roundDir string, dry bool) error {
	if adapterPath == "" {
		log.Println("  broadcast: base model round; no adapter to distribute")
		return nil
	}
	for _, n := range nodes {
		dst := fmt.Sprintf("%s@%s:%s/input/", n.SSHUser, n.Address, roundDir)
		if dry {
			log.Printf("  rsync %s %s", adapterPath, dst)
			continue
		}
		cmd := exec.Command("rsync", "-az", adapterPath, dst)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("rsync to %s: %w", n.Name, err)
		}
	}
	return nil
}

// triggerLocalTraining SSHes into each node and starts the local training job.
func triggerLocalTraining(nodes []FedNode, nsName, namespacesPath, baseAdapter,
	roundDir string, round, epochs int, algo string, proxMu float64, dry bool) error {
	var wg sync.WaitGroup
	errs := make([]error, len(nodes))
	for i, n := range nodes {
		outputDir := fmt.Sprintf("%s/%s/", roundDir, n.Name)
		args := []string{
			n.SSHUser + "@" + n.Address,
			"python3", "/app/python/train.py",
			"--mode", "repo",
			"--namespace", nsName,
			"--repo", n.Name,
			"--namespaces", namespacesPath,
			"--output-dir", outputDir,
			"--dataset-dir", fmt.Sprintf("/var/lib/aicluster/fed/%s/data/", n.Name),
			fmt.Sprintf("--fed-round=%d", round),
			fmt.Sprintf("--fed-epochs=%d", epochs),
			fmt.Sprintf("--fed-algo=%s", algo),
			fmt.Sprintf("--fed-proxmu=%.4f", proxMu),
		}
		if baseAdapter != "" {
			args = append(args, "--base-model", baseAdapter)
		}
		if dry {
			log.Printf("  ssh %s", strings.Join(args, " "))
			continue
		}
		wg.Add(1)
		go func(idx int, name string, a []string) {
			defer wg.Done()
			cmd := exec.Command("ssh", a...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				errs[idx] = fmt.Errorf("node %s: %w", name, err)
			}
		}(i, n.Name, args)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// waitForAdapters polls until each node has written its adapter file.
func waitForAdapters(nodes []FedNode, roundDir string, deadline time.Time, dry bool) error {
	if dry {
		log.Println("  wait: dry-run; skipping")
		return nil
	}
	pending := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		pending[n.Name] = true
	}
	for len(pending) > 0 {
		if time.Now().After(deadline) {
			var missing []string
			for name := range pending {
				missing = append(missing, name)
			}
			return fmt.Errorf("timeout waiting for adapters from: %s", strings.Join(missing, ", "))
		}
		for nodeName := range pending {
			path := filepath.Join(roundDir, nodeName, "adapter.safetensors")
			if _, err := os.Stat(path); err == nil {
				delete(pending, nodeName)
				log.Printf("  received adapter from %s", nodeName)
			}
		}
		if len(pending) > 0 {
			time.Sleep(30 * time.Second)
		}
	}
	return nil
}

// aggregateAdapters merges per-node adapters using the specified algorithm
// by invoking a Python helper (tools/fed_aggregate.py).
func aggregateAdapters(nodes []FedNode, roundDir, outputPath, algo string, proxMu float64, dry bool) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	args := []string{"tools/fed_aggregate.py",
		"--round-dir", roundDir,
		"--output", outputPath,
		"--algorithm", algo,
		fmt.Sprintf("--proxmu=%.4f", proxMu),
	}
	for _, n := range nodes {
		args = append(args, "--node", n.Name)
	}
	if dry {
		log.Printf("  python3 %s", strings.Join(args, " "))
		return nil
	}
	cmd := exec.Command("python3", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// loadFedNodes reads the inventory and returns nodes with federated=true label.
func loadFedNodes(path string) []FedNode {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("inventory: %v", err)
		return nil
	}

	var nodes []FedNode
	var current *FedNode
	lines := strings.Split(string(data), "\n")
	federated := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "- hostname:") || strings.HasPrefix(trim, "hostname:") {
			if current != nil && federated {
				nodes = append(nodes, *current)
			}
			current = &FedNode{}
			federated = false
			name := strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
			current.Name = strings.Trim(name, `"'`)
			if current.Name == "" {
				current = nil
			}
			continue
		}
		if current == nil {
			continue
		}
		kv := parseKV(trim)
		switch kv[0] {
		case "address":
			current.Address = kv[1]
		case "ssh_user":
			current.SSHUser = kv[1]
		case "federated":
			if kv[1] == "true" {
				federated = true
			}
		}
	}
	if current != nil && federated {
		nodes = append(nodes, *current)
	}
	return nodes
}

func parseKV(line string) [2]string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return [2]string{"", ""}
	}
	return [2]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
}
