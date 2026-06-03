// cmd/node-deploy orchestrates single-node deployment with auto-tuning of
// Ollama, SwarmUI, and training roles.
//
// Usage:
//
//	node-deploy [flags]
//
// Flags:
//
//	-roles       comma-separated list of roles (e.g. "chat,image-generation")
//	-dry-run     show what would be done without modifying the system
//	-inventory   path to cluster/inventory.yaml (default: cluster/inventory.yaml)
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"strings"

	"github.com/opd-ai/cluster/internal/autotuner"
	"github.com/opd-ai/cluster/internal/serviceinstall"
)

func main() {
	rolesFlag := flag.String("roles", "", "Comma-separated list of roles (e.g. 'chat,image-generation')")
	dryRun := flag.Bool("dry-run", false, "Show what would be done without modifying the system")
	_ = flag.String("inventory", "cluster/inventory.yaml", "Path to cluster/inventory.yaml")
	flag.Parse()

	if *rolesFlag == "" {
		log.Fatal("--roles flag is required (e.g. 'chat' or 'chat,image-generation')")
	}

	roles := strings.Split(*rolesFlag, ",")
	for i := range roles {
		roles[i] = strings.TrimSpace(roles[i])
	}

	// Probe local hardware
	hw, err := autotuner.Probe()
	if err != nil {
		log.Fatalf("Hardware probe failed: %v", err)
	}

	fmt.Printf("=== Local Hardware Profile ===\n")
	fmt.Printf("Accelerator: %s\n", hw.Accelerator)
	fmt.Printf("VRAM: %d GB\n", hw.VramGB)
	fmt.Printf("RAM: %d GB\n", hw.RamGB)
	fmt.Printf("Disk: %d GB\n", hw.DiskGB)
	fmt.Printf("CPUs: %d\n", hw.NumCPUs)

	// Split resources among roles
	budgets := autotuner.BudgetSplit(roles, hw, nil)

	fmt.Printf("\n=== Resource Budgets ===\n")
	for role, budget := range budgets {
		fmt.Printf("%s: %d GB VRAM, %d GB RAM, %d CPUs\n",
			role, budget.VramGB, budget.RamGB, budget.NumCPUs)
	}

	// Generate service units for each role
	portMap := rolePortMap()
	units := []*serviceinstall.SystemdUnit{}

	for _, role := range roles {
		budget := budgets[role]
		port := portMap[role]

		var unit *serviceinstall.SystemdUnit

		switch role {
		case "chat":
			ollamaConfig := autotuner.OllamaConfig(role, budget, port)
			unit = &serviceinstall.SystemdUnit{
				Name:        fmt.Sprintf("ollama-%s", role),
				Description: fmt.Sprintf("Ollama (%s role)", role),
				Executable:  "ollama",
				Environment: ollamaConfig.EnvVars(),
			}

		case "image-generation":
			swarmConfig := autotuner.SwarmUIConfig(budget, port)
			unit = &serviceinstall.SystemdUnit{
				Name:        fmt.Sprintf("swarmui-%s", role),
				Description: fmt.Sprintf("SwarmUI (%s role)", role),
				Executable:  "swarmui",
				Args: []string{
					fmt.Sprintf("--port=%d", swarmConfig.Port),
				},
			}

		case "training":
			trainingConfig := autotuner.TrainingConfig(budget, port)
			unit = &serviceinstall.SystemdUnit{
				Name:        fmt.Sprintf("training-%s", role),
				Description: fmt.Sprintf("Training service (%s role)", role),
				Executable:  "training-daemon",
				Args: []string{
					fmt.Sprintf("--port=%d", trainingConfig.Port),
					fmt.Sprintf("--mode=%s", trainingConfig.TrainingMode),
				},
				Environment: map[string]string{
					"TRAINING_MAX_GPU_MEMORY": fmt.Sprintf("%d", trainingConfig.MaxGPUMemory),
					"TRAINING_MAX_CPU_MEMORY": fmt.Sprintf("%d", trainingConfig.MaxCPUMemory),
				},
			}

		default:
			log.Printf("Warning: unknown role %q, skipping unit generation", role)
			continue
		}

		if unit != nil {
			units = append(units, unit)
		}
	}

	// Write unit files
	fmt.Printf("\n=== Service Units ===\n")
	for _, unit := range units {
		var path string
		var err error

		if runtime.GOOS == "darwin" {
			path, err = writeDarwinUnit(unit, *dryRun)
		} else {
			path, err = serviceinstall.WriteLinuxUnit(unit, *dryRun)
		}

		if err != nil {
			log.Printf("Failed to write unit %s: %v", unit.Name, err)
			continue
		}

		fmt.Printf("✓ %s -> %s\n", unit.Name, path)
	}

	fmt.Printf("\n=== Deployment Complete ===\n")
	if *dryRun {
		fmt.Println("(dry-run: no actual changes made)")
	} else {
		fmt.Println("Run 'systemctl daemon-reload' to load new units.")
		fmt.Println("Run 'systemctl enable --now <unit>' to start a service.")
	}
}

// rolePortMap returns default port assignments for roles.
func rolePortMap() map[string]int {
	return map[string]int{
		"chat":              11434,
		"image-generation": 7860,
		"training":          8888,
	}
}

// writeDarwinUnit is a placeholder; the real implementation is in serviceinstall/darwin.go
// but requires build tags which we avoid here.
func writeDarwinUnit(unit *serviceinstall.SystemdUnit, dryRun bool) (string, error) {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write launchd plist for %s\n", unit.Name)
		return fmt.Sprintf("~/Library/LaunchAgents/com.opd.%s.plist", unit.Name), nil
	}
	log.Fatalf("darwin service installation not yet implemented")
	return "", nil
}
