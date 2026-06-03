package autotuner

// SwarmUIArgs represents command-line arguments for SwarmUI.
type SwarmUIArgs struct {
	Port      int
	DeviceID  int // GPU device index (if applicable)
	MaxMemory int // max memory in MB
}

// SwarmUIConfig generates SwarmUI configuration for a role's budget.
func SwarmUIConfig(budget ResourceBudget, port int) SwarmUIArgs {
	return SwarmUIArgs{
		Port:      port,
		DeviceID:  0,
		MaxMemory: budget.VramGB * 1024, // convert GB to MB
	}
}
