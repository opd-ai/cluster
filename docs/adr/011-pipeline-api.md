# ADR 011 — Multi-Stage Pipeline API

**Status:** Proposed  
**Date:** 2026-06-03

## Context

The cluster architecture supports single-stage inference requests (e.g., chat completion or image generation). However, many practical workflows require chaining multiple services: for example, a user prompt is first processed by a chat model to refine a description, and then the resulting text is fed to an image generation service. Supporting such workflows efficiently requires a multi-stage pipeline execution engine and a corresponding HTTP API on the gateway.

## Decision

**Implement a multi-stage pipeline executor and HTTP API that allows clients to chain inference stages serially, passing output from one stage as input to the next.**

- Define `pipeline.PipelineSpec`, `pipeline.Stage`, and `pipeline.StageResult` types with role, model, and config per stage.
- Implement `pipeline.Executor` to (1) pick a backend for each stage, (2) submit the stage to the node-agent's `/api/v1/pipeline/submit` endpoint, (3) poll `/api/v1/pipeline/result/{id}` for completion.
- Add `POST /v1/pipelines` endpoint to the gateway to accept a `PipelineSpec` and return a `PipelineExecution` with status and results.
- Stages execute serially; each stage's output becomes the input for the next stage (unless explicitly overridden).

## Rationale

### Problem: No Multi-Stage Support

- Current gateway routes single-stage inference only; complex workflows require manual orchestration or external tooling.
- Applications that need chained inference (e.g., summarize-then-generate-image) must make separate API calls and manage state.

### Solution: Standardized Pipeline Execution

1. **Declarative Spec**: Client defines a pipeline in one request; gateway handles routing, backend selection, and inter-stage data flow.
2. **Backend-Agnostic**: Each stage can run on different nodes; gateway uses the registry to find suitable backends.
3. **Timeout & Error Handling**: Per-stage timeouts; if a stage fails, pipeline stops and returns the error.
4. **Real-Time Status**: Console can subscribe to `MsgPipelineState` WebSocket messages for live progress tracking.

## Consequences

### Positive

- Workflows like "chat → image-generation" are a single API call.
- Extensible: future versions can support parallel stages, conditional branching, or looping.
- Reduces operational overhead for applications that need stage chaining.

### Negative (to address in Phase 5+)

- Pipeline execution is synchronous in the gateway; long pipelines block the request. Future versions should support async submission with polling/webhooks.
- No persistent storage of pipeline definitions or results yet; all state is in-memory and lost on gateway restart.
- Per-stage timeout validation is not yet enforced; stages may hang indefinitely if a backend is unresponsive.

## Implementation Notes

- `cmd/node-agent` implements `POST /api/v1/pipeline/submit` and `GET /api/v1/pipeline/result/{id}` for single-stage execution.
- `cmd/gateway` implements `POST /v1/pipelines` which orchestrates the multi-stage flow via the `pipeline.Executor`.
- The executor uses `internal/lb.BackendRegistry` to select backends per role, enabling multi-role routing.
- WebSocket push `MsgPipelineState` allows console clients to see real-time pipeline progress.
