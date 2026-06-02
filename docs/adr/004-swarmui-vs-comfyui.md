# ADR 004 — Image Generation: SwarmUI vs Raw ComfyUI

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The cluster needs a stable API surface for image (and video) generation,
accessible via the gateway's `/v1/images/generations` endpoint. Two candidates:
**SwarmUI** (wraps ComfyUI with a management API) and **raw ComfyUI** (direct
workflow API).

## Decision

**Default to SwarmUI** as the primary image generation backend.
ComfyUI is retained as a secondary backend for advanced custom workflows.

## Rationale

### SwarmUI (default)

- Provides a stable REST API for text-to-image that maps cleanly to the
  OpenAI `/v1/images/generations` schema.
- Model management UI simplifies checkpoint and LoRA installation without
  SSH access.
- Health check endpoint (`/API/ServerStatus`) is used by the gateway for
  backend discovery.
- Abstracts ComfyUI workflow complexity for the majority of use cases
  (SDXL, Flux, video checkpoints via SwarmUI video extension).

### Raw ComfyUI (secondary)

ComfyUI is retained for:
- Custom multi-stage workflows (e.g., ControlNet pipelines, inpaint/outpaint).
- Advanced video generation with complex node graphs.
- Operators who prefer workflow-level control.

ComfyUI listens on `:8188`; the gateway can route to it via a custom
`workflow_hash` parameter in the generation request.

## Consequences

- `cmd/gateway` health checks `/API/ServerStatus` for SwarmUI and
  `/system_stats` for ComfyUI.
- The default `POST /v1/images/generations` payload is translated to
  SwarmUI's `GenerateImageWS` API.
- Custom ComfyUI workflows are passed as opaque JSON blobs in the `workflow`
  field of the generation request.
