package autotuner

// ResourceBudget represents a resource allocation for a single role.
type ResourceBudget struct {
	VramGB  int    // GPU VRAM allocation in GB
	RamGB   int    // CPU RAM allocation in GB
	NumCPUs int    // CPU cores allocation
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
		"training":        16,
		"chat":            4,
		"image-generation": 8,
	}

	// Apply operator overrides
	for role, vram := range overrides {
		roleMinVram[role] = vram
	}

	// Calculate total required VRAM
	totalRequired := 0
	for _, role := range roles {
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
	for i, role := range roles {
		minVram := roleMinVram[role]
		allocated := int(float64(minVram) * vramScale)
		if allocated < 1 && hw.VramGB > 0 {
			allocated = 1
		}

		// Last role gets any remainder
		if i == len(roles)-1 {
			allocated = hw.VramGB - allocatedVram
		}

		if allocated < 0 {
			allocated = 0
		}

		allocatedVram += allocated

		budgets[role] = ResourceBudget{
			VramGB:  allocated,
			RamGB:   hw.RamGB / len(roles), // Divide RAM equally
			NumCPUs: hw.NumCPUs / len(roles), // Divide CPUs equally
		}
	}

	return budgets
}
