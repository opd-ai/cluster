// Package pipeline provides multi-stage pipeline execution for generative workloads.
package pipeline

import (
	"time"
)

// PipelineSpec defines a multi-stage pipeline to be executed.
type PipelineSpec struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Stages     []Stage       `json:"stages"`
	CreatedAt  time.Time     `json:"created_at"`
}

// Stage represents a single stage in a pipeline.
type Stage struct {
	ID       string            `json:"id"`
	Index    int               `json:"index"`
	Role     string            `json:"role"`          // e.g., "chat", "image-generation"
	Model    string            `json:"model"`         // e.g., "llama2", "stable-diffusion-xl"
	Input    StageInput        `json:"input"`
	Config   map[string]any    `json:"config"`        // role-specific config (e.g., temperature, steps)
	Timeout  time.Duration     `json:"timeout"`       // per-stage timeout
}

// StageInput specifies the input for a stage.
type StageInput struct {
	// FromPrevious indicates that input comes from the previous stage's output.
	FromPrevious bool `json:"from_previous"`
	// Direct is the literal input if not FromPrevious.
	Direct any `json:"direct,omitempty"`
}

// StageResult holds the result of executing a stage.
type StageResult struct {
	ID         string        `json:"id"`
	Index      int           `json:"index"`
	Role       string        `json:"role"`
	Status     string        `json:"status"`         // pending, running, completed, failed
	Output     any           `json:"output"`
	Error      string        `json:"error,omitempty"`
	Progress   float64       `json:"progress"`       // 0-1
	StartedAt  time.Time     `json:"started_at"`
	CompletedAt time.Time    `json:"completed_at,omitempty"`
}

// PipelineExecution tracks the execution state of a pipeline.
type PipelineExecution struct {
	PipelineID string          `json:"pipeline_id"`
	Status     string          `json:"status"`         // pending, running, completed, failed
	Results    []StageResult   `json:"results"`
	StartedAt  time.Time       `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Error      string          `json:"error,omitempty"`
}
