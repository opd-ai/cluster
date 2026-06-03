package autotuner

// TrainingEnv represents environment variables for training jobs.
type TrainingEnv struct {
	MaxGPUMemory int
	MaxCPUMemory int
	Port         int
	TrainingMode string // "lora", "full", "quantized"
}

// TrainingConfig generates training environment configuration for a role's budget.
func TrainingConfig(budget ResourceBudget, port int) TrainingEnv {
	// Determine training mode based on available resources
	mode := "quantized" // most conservative
	if budget.VramGB >= 16 {
		mode = "full" // full fine-tuning
	} else if budget.VramGB >= 8 {
		mode = "lora" // LoRA fine-tuning
	}

	return TrainingEnv{
		MaxGPUMemory: budget.VramGB * 1024, // Convert GB to MB
		MaxCPUMemory: budget.RamGB * 1024,  // Convert GB to MB
		Port:         port,
		TrainingMode: mode,
	}
}
