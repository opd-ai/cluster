package autotuner

import "fmt"

// OllamaEnv represents environment variables for Ollama configuration.
type OllamaEnv struct {
	NumGPU  int    // number of GPU layers to offload
	NumCtx  int    // context window size
	RopeF32 string // ROPE frequency base
	Port    int    // listening port
}

// OllamaConfig generates Ollama configuration for a role's budget.
// Assumes Ollama will run on the specified port for this role.
func OllamaConfig(role string, budget ResourceBudget, port int) OllamaEnv {
	config := OllamaEnv{
		Port:    port,
		RopeF32: "1", // default: fp32 rope
	}

	// Determine number of GPU layers to offload based on VRAM
	// Rough heuristic: 2GB per GPU layer
	if budget.VramGB > 0 {
		config.NumGPU = budget.VramGB / 2
	}

	// Context window sizing
	// Smaller context for roles with limited resources
	switch {
	case budget.VramGB >= 16:
		config.NumCtx = 4096 // large context
	case budget.VramGB >= 8:
		config.NumCtx = 2048 // medium context
	default:
		config.NumCtx = 1024 // small context
	}

	return config
}

// OllamaEnvVars returns Ollama environment variables as a map suitable for systemd env.
func (c OllamaEnv) EnvVars() map[string]string {
	return map[string]string{
		"OLLAMA_NUM_GPU": fmt.Sprintf("%d", c.NumGPU),
		"OLLAMA_NUM_CTX": fmt.Sprintf("%d", c.NumCtx),
		"OLLAMA_ROPE_F32": c.RopeF32,
	}
}
