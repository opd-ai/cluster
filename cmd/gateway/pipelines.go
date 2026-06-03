// Package gateway provides HTTP API routing and load balancing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/opd-ai/cluster/internal/lb"
	"github.com/opd-ai/cluster/internal/pipeline"
)

// pipelineExecutor manages pipeline execution via the load balancer.
type pipelineExecutor struct {
	registry *lb.BackendRegistry
	mu       sync.RWMutex
}

// NewPipelineExecutor creates a new pipeline executor.
func NewPipelineExecutor(registry *lb.BackendRegistry) *pipelineExecutor {
	return &pipelineExecutor{
		registry: registry,
	}
}

// Execute runs a pipeline spec.
func (pe *pipelineExecutor) Execute(ctx context.Context, spec *pipeline.PipelineSpec) (*pipeline.PipelineExecution, error) {
	// Use the actual executor from the pipeline package
	executor := pipeline.NewExecutor(pe.registry)
	return executor.Execute(ctx, spec)
}

// handlePostPipelines handles POST /v1/pipelines to start a pipeline execution.
func (gw *Gateway) handlePostPipelines(w http.ResponseWriter, r *http.Request) {
	var spec pipeline.PipelineSpec
	err := json.NewDecoder(r.Body).Decode(&spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if spec.ID == "" {
		spec.ID = fmt.Sprintf("pipeline-%d", gw.pipelineIDCounter.Add(1))
	}

	// Execute the pipeline asynchronously
	exec, err := gw.pipelineExecutor.Execute(r.Context(), &spec)
	if err != nil {
		http.Error(w, fmt.Sprintf("pipeline execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(exec)
}

// handleGetPipelineStatus handles GET /v1/pipelines/{id} to check pipeline status.
func (gw *Gateway) handleGetPipelineStatus(w http.ResponseWriter, r *http.Request) {
	pipelineID := chi.URLParam(r, "id")
	if pipelineID == "" {
		http.Error(w, "pipeline ID is required", http.StatusBadRequest)
		return
	}

	// TODO: Retrieve pipeline status from storage/cache
	// For now, return a placeholder
	status := map[string]interface{}{
		"id":     pipelineID,
		"status": "completed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
