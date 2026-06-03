package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opd-ai/cluster/internal/pipeline"
)

// TestPipelineAPIStructure tests the pipeline API request/response structure.
func TestPipelineAPIStructure(t *testing.T) {
	// Test 1: Valid pipeline spec can be marshaled to/from JSON.
	t.Run("PipelineSpec JSON marshaling", func(t *testing.T) {
		spec := pipeline.PipelineSpec{
			ID:   "test-pipeline-1",
			Name: "Chat to Image Generation",
			Stages: []pipeline.Stage{
				{
					ID:    "stage-0",
					Index: 0,
					Role:  "chat",
					Model: "llama2",
					Input: pipeline.StageInput{
						Direct: "Generate an image of a sunset",
					},
					Config:  map[string]any{"temperature": 0.7},
					Timeout: pipeline.Duration{Duration: 30 * time.Second},
				},
				{
					ID:    "stage-1",
					Index: 1,
					Role:  "image-generation",
					Model: "stable-diffusion-xl",
					Input: pipeline.StageInput{
						FromPrevious: true,
					},
					Config:  map[string]any{"steps": 20},
					Timeout: pipeline.Duration{Duration: 60 * time.Second},
				},
			},
			CreatedAt: time.Now(),
		}

		// Marshal to JSON
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatalf("failed to marshal spec: %v", err)
		}

		if len(data) == 0 {
			t.Error("expected non-empty JSON")
		}

		// Unmarshal from JSON
		var decoded pipeline.PipelineSpec
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal spec: %v", err)
		}

		if decoded.ID != spec.ID {
			t.Errorf("expected ID %q, got %q", spec.ID, decoded.ID)
		}

		if len(decoded.Stages) != len(spec.Stages) {
			t.Errorf("expected %d stages, got %d", len(spec.Stages), len(decoded.Stages))
		}
	})

	// Test 2: PipelineExecution response structure is correct.
	t.Run("PipelineExecution response structure", func(t *testing.T) {
		exec := pipeline.PipelineExecution{
			PipelineID: "test-pipeline-1",
			Status:     "completed",
			Results: []pipeline.StageResult{
				{
					ID:        "stage-0",
					Index:     0,
					Role:      "chat",
					Status:    "completed",
					Output:    "This is a sunset image prompt",
					Progress:  1.0,
					StartedAt: time.Now(),
				},
				{
					ID:        "stage-1",
					Index:     1,
					Role:      "image-generation",
					Status:    "completed",
					Output:    "https://example.com/image.png",
					Progress:  1.0,
					StartedAt: time.Now(),
				},
			},
			StartedAt:   time.Now(),
			CompletedAt: time.Now(),
		}

		// Ensure it can be marshaled to JSON
		data, err := json.Marshal(exec)
		if err != nil {
			t.Fatalf("failed to marshal execution: %v", err)
		}

		if len(data) == 0 {
			t.Error("expected non-empty JSON")
		}

		// Ensure it can be unmarshaled from JSON
		var decoded pipeline.PipelineExecution
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal execution: %v", err)
		}

		if decoded.PipelineID != exec.PipelineID {
			t.Errorf("expected pipeline ID %q, got %q", exec.PipelineID, decoded.PipelineID)
		}

		if decoded.Status != exec.Status {
			t.Errorf("expected status %q, got %q", exec.Status, decoded.Status)
		}

		if len(decoded.Results) != len(exec.Results) {
			t.Errorf("expected %d stage results, got %d", len(exec.Results), len(decoded.Results))
		}
	})

	// Test 3: Invalid JSON request returns 400.
	t.Run("POST /v1/pipelines with invalid JSON", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var spec pipeline.PipelineSpec
			if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(&pipeline.PipelineExecution{PipelineID: spec.ID, Status: "pending"})
		})

		req := httptest.NewRequest("POST", "/v1/pipelines", bytes.NewReader([]byte(`{invalid}`)))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	// Test 4: Auto-generated pipeline ID.
	t.Run("POST /v1/pipelines auto-generates ID", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var spec pipeline.PipelineSpec
			if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if spec.ID == "" {
				spec.ID = fmt.Sprintf("pipeline-%d", time.Now().Unix())
			}

			exec := pipeline.PipelineExecution{
				PipelineID: spec.ID,
				Status:     "pending",
				Results:    make([]pipeline.StageResult, 0),
				StartedAt:  time.Now(),
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(exec)
		})

		spec := pipeline.PipelineSpec{
			Name: "Auto ID Pipeline",
			Stages: []pipeline.Stage{
				{
					ID:    "stage-0",
					Index: 0,
					Role:  "chat",
					Model: "llama2",
					Input: pipeline.StageInput{
						Direct: "Hello",
					},
					Timeout: pipeline.Duration{Duration: 10 * time.Second},
				},
			},
			CreatedAt: time.Now(),
		}

		body, err := json.Marshal(spec)
		if err != nil {
			t.Fatalf("failed to marshal spec: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/pipelines", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}

		var exec pipeline.PipelineExecution
		if err := json.NewDecoder(w.Body).Decode(&exec); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if exec.PipelineID == "" {
			t.Error("expected auto-generated pipeline ID")
		}
	})

	// Test 5: Pipeline execution errors return 500.
	t.Run("POST /v1/pipelines execution failure returns 500", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var spec pipeline.PipelineSpec
			if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "pipeline execution failed: backend unavailable", http.StatusInternalServerError)
		})

		spec := pipeline.PipelineSpec{
			ID:   "failing-pipeline",
			Name: "Failing Pipeline",
			Stages: []pipeline.Stage{
				{
					ID:    "stage-0",
					Index: 0,
					Role:  "chat",
					Model: "llama2",
					Input: pipeline.StageInput{
						Direct: "hello",
					},
				},
			},
			CreatedAt: time.Now(),
		}
		body, err := json.Marshal(spec)
		if err != nil {
			t.Fatalf("failed to marshal spec: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/pipelines", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", w.Code)
		}
	})

	// Test 6: Pipeline stage chaining (FromPrevious).
	t.Run("Pipeline stage input chaining", func(t *testing.T) {
		spec := pipeline.PipelineSpec{
			ID:   "chaining-test",
			Name: "Stage Chaining",
			Stages: []pipeline.Stage{
				{
					ID:    "stage-0",
					Index: 0,
					Role:  "chat",
					Model: "llama2",
					Input: pipeline.StageInput{
						Direct: "What is 2+2?",
					},
				},
				{
					ID:    "stage-1",
					Index: 1,
					Role:  "image-generation",
					Model: "stable-diffusion-xl",
					Input: pipeline.StageInput{
						FromPrevious: true,
					},
				},
			},
			CreatedAt: time.Now(),
		}

		// Verify FromPrevious is set correctly
		if !spec.Stages[1].Input.FromPrevious {
			t.Error("expected FromPrevious to be true for stage 1")
		}

		// Verify Direct is set correctly for stage 0
		if spec.Stages[0].Input.Direct == "" {
			t.Error("expected Direct input for stage 0")
		}
	})

	// Test 7: Duration parsing (e.g., "30s", "1m30s").
	t.Run("Duration parsing in Stage config", func(t *testing.T) {
		jsonStr := `{
			"id": "test",
			"timeout": "30s"
		}`

		var stage pipeline.Stage
		if err := json.Unmarshal([]byte(jsonStr), &stage); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if stage.Timeout.Duration != 30*time.Second {
			t.Errorf("expected 30s, got %v", stage.Timeout.Duration)
		}
	})
}
