// Package pipeline provides multi-stage pipeline execution for generative workloads.
package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/opd-ai/cluster/internal/lb"
	"github.com/opd-ai/cluster/internal/nodeapi"
)

// Executor runs a pipeline by executing stages serially on appropriate backends.
type Executor struct {
	registry *lb.BackendRegistry
	client   *http.Client
}

// NewExecutor creates a new pipeline executor.
func NewExecutor(registry *lb.BackendRegistry) *Executor {
	return &Executor{
		registry: registry,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Execute runs a pipeline spec from start to finish.
// Stages are executed serially; output from stage N is input to stage N+1.
func (e *Executor) Execute(ctx context.Context, spec *PipelineSpec) (*PipelineExecution, error) {
	exec := &PipelineExecution{
		PipelineID: spec.ID,
		Status:     "running",
		StartedAt:  time.Now(),
		Results:    make([]StageResult, len(spec.Stages)),
	}

	var prevOutput any

	for i, stage := range spec.Stages {
		// Determine input for this stage
		stageInput := prevOutput
		if !stage.Input.FromPrevious {
			stageInput = stage.Input.Direct
		}

		// Pick a backend for this stage
		backend := e.registry.Pick(stage.Role, stage.Model, "")
		if backend == nil {
			err := fmt.Errorf("no healthy backend available for role=%s model=%s", stage.Role, stage.Model)
			result := StageResult{
				ID:        stage.ID,
				Index:     stage.Index,
				Role:      stage.Role,
				Status:    "failed",
				Error:     err.Error(),
				StartedAt: time.Now(),
			}
			exec.Results[i] = result
			exec.Status = "failed"
			exec.Error = err.Error()
			return exec, err
		}

		// Apply per-stage timeout if specified.
		stageCtx := ctx
		var stageCancel context.CancelFunc
		if stage.Timeout.Duration > 0 {
			stageCtx, stageCancel = context.WithTimeout(ctx, stage.Timeout.Duration)
		} else {
			stageCtx, stageCancel = context.WithCancel(ctx)
		}

		// Execute the stage on the selected backend
		result, err := e.executeStage(stageCtx, backend, stage, stageInput)
		stageCancel()
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			exec.Results[i] = result
			exec.Status = "failed"
			exec.Error = err.Error()
			return exec, err
		}

		exec.Results[i] = result
		prevOutput = result.Output
	}

	exec.Status = "completed"
	exec.CompletedAt = time.Now()
	return exec, nil
}

// executeStage submits a single stage to a backend and waits for completion.
func (e *Executor) executeStage(ctx context.Context, backend *lb.BackendRecord, stage Stage, input any) (StageResult, error) {
	result := StageResult{
		ID:        stage.ID,
		Index:     stage.Index,
		Role:      stage.Role,
		Status:    "pending",
		StartedAt: time.Now(),
	}

	// Find the port for this role
	var port string
	for _, svc := range backend.Services {
		if svc.Role == stage.Role {
			port = svc.Port
			break
		}
	}
	if port == "" {
		return result, fmt.Errorf("no service binding found for role %s on backend %s", stage.Role, backend.Address)
	}

	// Prepare pipeline submit request
	submitReq := struct {
		StageID string         `json:"stage_id"`
		Role    string         `json:"role"`
		Model   string         `json:"model"`
		Input   any            `json:"input"`
		Config  map[string]any `json:"config"`
	}{
		StageID: stage.ID,
		Role:    stage.Role,
		Model:   stage.Model,
		Input:   input,
		Config:  stage.Config,
	}

	submitBody, err := json.Marshal(submitReq)
	if err != nil {
		return result, fmt.Errorf("encode submit request: %w", err)
	}

	// POST to /api/v1/pipeline/submit
	url := fmt.Sprintf("http://%s:%s/api/v1/pipeline/submit", backend.Address, port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(submitBody))
	if err != nil {
		return result, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("submit stage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return result, fmt.Errorf("submit failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var ack nodeapi.PipelineAck
	err = json.NewDecoder(resp.Body).Decode(&ack)
	if err != nil {
		return result, fmt.Errorf("decode ack: %w", err)
	}

	result.Status = "running"

	// Poll for results (with exponential backoff)
	jobID := ack.JobID
	pollURL := fmt.Sprintf("http://%s:%s/api/v1/pipeline/result/%s", backend.Address, port, jobID)
	backoff := 100 * time.Millisecond
	maxRetries := 360            // ~30 min at max backoff (5s)
	maxPollDuration := time.Hour  // Cap total polling time at 1 hour
	pollStart := time.Now()
	retries := 0

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(backoff):
		}

		// Check if total polling time has exceeded max
		if time.Since(pollStart) > maxPollDuration {
			return result, fmt.Errorf("polling timeout: exceeded max duration of %v", maxPollDuration)
		}

		// Increase backoff, cap at 5s
		if backoff < 5*time.Second {
			backoff = time.Duration(float64(backoff) * 1.5)
		}

		pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return result, fmt.Errorf("create poll request: %w", err)
		}

		pollResp, err := e.client.Do(pollReq)
		if err != nil {
			// Transient network error; retry
			retries++
			if retries > maxRetries {
				return result, fmt.Errorf("polling failed: exceeded max retries after network error: %w", err)
			}
			continue
		}

		// Check status code: distinguish permanent vs transient errors
		if pollResp.StatusCode == http.StatusNotFound {
			// Job not found — permanent error
			pollResp.Body.Close()
			return result, fmt.Errorf("polling failed: job not found (404)")
		}

		if pollResp.StatusCode >= 400 && pollResp.StatusCode < 500 {
			// Other 4xx error (except 404, already handled) — permanent error
			body, _ := io.ReadAll(pollResp.Body)
			pollResp.Body.Close()
			return result, fmt.Errorf("polling failed: permanent error %d: %s", pollResp.StatusCode, string(body))
		}

		if pollResp.StatusCode != http.StatusOK {
			// 5xx or other non-2xx — transient error, retry
			pollResp.Body.Close()
			retries++
			if retries > maxRetries {
				return result, fmt.Errorf("polling failed: exceeded max retries with status %d", pollResp.StatusCode)
			}
			continue
		}

		var pollResult nodeapi.PipelineResult
		err = json.NewDecoder(pollResp.Body).Decode(&pollResult)
		pollResp.Body.Close()

		if err != nil {
			// Decode error — transient issue
			retries++
			if retries > maxRetries {
				return result, fmt.Errorf("polling failed: exceeded max retries on decode: %w", err)
			}
			continue
		}

		result.Status = pollResult.Status
		result.Progress = pollResult.Progress
		result.Output = pollResult.Output
		if pollResult.Error != "" {
			result.Error = pollResult.Error
		}

		// Done if completed or failed
		if pollResult.Status == "completed" || pollResult.Status == "failed" {
			result.CompletedAt = time.Now()
			break
		}
	}

	if result.Status == "failed" {
		return result, fmt.Errorf("stage failed: %s", result.Error)
	}

	return result, nil
}
