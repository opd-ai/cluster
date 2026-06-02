# ADR 002 — LLM Runtime: Ollama vs vLLM

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The cluster needs an LLM inference runtime that supports hot-swap (load/unload
models without restarting the server), runs on heterogeneous hardware (NVIDIA,
AMD ROCm, Apple Silicon), and is operable via a simple REST API.

Two realistic candidates: **Ollama** and **vLLM**.

## Decision

**Default to Ollama** on all node types.

vLLM is documented below as an alternative for operators who need higher
throughput on homogeneous NVIDIA-only fleets.

## Rationale

### Ollama (default)

- Single static binary; zero runtime dependencies beyond the NVIDIA or Metal
  driver.
- Runs natively on Apple Silicon (Metal backend) without Docker.
- REST API (`/api/generate`, `/api/chat`, `/api/embeddings`) requires no
  adapter layer in `cmd/gateway`.
- Model hot-swap: `ollama pull` + `ollama rm` while the server is running.
  The gateway re-reads the inventory on SIGHUP and routes to the new model.
- Supports GGUF quantisation (2-bit through 8-bit), fitting 70B models on
  consumer VRAM.
- Community-maintained model library with checksummed downloads.

### vLLM (alternative)

vLLM is the right choice when:
- The fleet is NVIDIA-only and maximising throughput (tokens/s) per GPU
  is the primary goal.
- PagedAttention is needed for very long contexts (> 32 K tokens).
- Speculative decoding at the engine level is preferred over gateway-level
  routing.

To use vLLM, replace Ollama with vLLM in the node provisioning step
(`docs/ollama-daemon-setup.md` → equivalent vLLM systemd service), and
update `cmd/gateway` to use the vLLM-compatible OpenAI endpoint path
(`/v1/chat/completions` — already compatible).

## Consequences

- `cmd/gateway` assumes Ollama's `/api/tags` endpoint for model discovery.
- Apple Silicon nodes are always Ollama; vLLM is Linux/CUDA only.
- Hot-swap relies on Ollama's model management; vLLM operators must handle
  model management differently.
