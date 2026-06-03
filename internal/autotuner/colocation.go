package autotuner

import "strings"

// ResourceBudget represents a resource allocation for a single role.
type ResourceBudget struct {
	VramGB  int // GPU VRAM allocation in GB
	RamGB   int // CPU RAM allocation in GB
	NumCPUs int // CPU cores allocation
}

// BudgetSplit divides available resources among multiple roles.
// Returns a map of role → ResourceBudget.
// Respects operator overrides and applies sensible defaults.
func BudgetSplit(roles []string, hw *HardwareProfile, overrides map[string]int) map[string]ResourceBudget {
	budgets := make(map[string]ResourceBudget)

	if len(roles) == 0 {
		return budgets
	}

	// Role-specific VRAM requirements (in GB)
	roleMinVram := map[string]int{
		"training":         16,
		"chat":             4,
		"image-generation": 8,
	}

	// Apply operator overrides
	for role, vram := range overrides {
		roleMinVram[role] = vram
	}

	filteredRoles := make([]string, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		normalized := strings.TrimSpace(role)
		if normalized == "" {
			continue
		}
		if _, ok := roleMinVram[normalized]; !ok {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		filteredRoles = append(filteredRoles, normalized)
	}

	if len(filteredRoles) == 0 {
		return budgets
	}

	// Calculate total required VRAM
	totalRequired := 0
	for _, role := range filteredRoles {
		if minVram, ok := roleMinVram[role]; ok {
			totalRequired += minVram
		}
	}

	// If we don't have enough VRAM, scale down allocations proportionally
	vramScale := 1.0
	if totalRequired > hw.VramGB {
		vramScale = float64(hw.VramGB) / float64(totalRequired)
	}

	// Allocate VRAM per role
	allocatedVram := 0
	for _, role := range filteredRoles {
		minVram := roleMinVram[role]
		remainingVram := hw.VramGB - allocatedVram
		
		allocated := int(float64(minVram) * vramScale)
		if allocated < 1 && remainingVram > 0 {
			allocated = 1
		}
		if allocated > remainingVram {
			allocated = remainingVram
		}

		allocatedVram += allocated

		budgets[role] = ResourceBudget{
			VramGB:  allocated,
			RamGB:   hw.RamGB / len(filteredRoles),   // Divide RAM equally
			NumCPUs: hw.NumCPUs / len(filteredRoles), // Divide CPUs equally
		}
	}

	return budgets
}
