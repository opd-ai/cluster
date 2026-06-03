// Package pipeline provides multi-stage pipeline execution for generative workloads.
package pipeline

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that marshals to/from a JSON string (e.g., "30s", "1m30s").
type Duration struct {
	time.Duration
}

// MarshalJSON encodes the duration as a string (e.g., "30s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalJSON decodes a duration string (e.g., "30s") or numeric nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		d.Duration = dur
		return nil
	}
	// Fall back to numeric nanoseconds
	var ns int64
	if err := json.Unmarshal(b, &ns); err != nil {
		return fmt.Errorf("duration must be a string (e.g. \"30s\") or integer nanoseconds")
	}
	d.Duration = time.Duration(ns)
	return nil
}

// PipelineSpec defines a multi-stage pipeline to be executed.
type PipelineSpec struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Stages    []Stage   `json:"stages"`
	CreatedAt time.Time `json:"created_at"`
}

// Stage represents a single stage in a pipeline.
type Stage struct {
	ID      string         `json:"id"`
	Index   int            `json:"index"`
	Role    string         `json:"role"`  // e.g., "chat", "image-generation"
	Model   string         `json:"model"` // e.g., "llama2", "stable-diffusion-xl"
	Input   StageInput     `json:"input"`
	Config  map[string]any `json:"config"`  // role-specific config (e.g., temperature, steps)
	Timeout Duration       `json:"timeout"` // per-stage timeout (e.g., "30s")
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
	ID          string    `json:"id"`
	Index       int       `json:"index"`
	Role        string    `json:"role"`
	Status      string    `json:"status"` // pending, running, completed, failed
	Output      any       `json:"output"`
	Error       string    `json:"error,omitempty"`
	Progress    float64   `json:"progress"` // 0-1
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// PipelineExecution tracks the execution state of a pipeline.
type PipelineExecution struct {
	PipelineID  string        `json:"pipeline_id"`
	Status      string        `json:"status"` // pending, running, completed, failed
	Results     []StageResult `json:"results"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at,omitempty"`
	Error       string        `json:"error,omitempty"`
}
