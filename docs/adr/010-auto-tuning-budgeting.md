# ADR 010 — Auto-Tuning and Resource Budgeting

**Status:** Proposed  
**Date:** 2026-06-03

## Context

When deploying multiple roles (e.g., `chat` + `image-generation`) on a single host, resource contention is inevitable. The system must intelligently partition GPU VRAM, CPU cores, and RAM across roles while allowing operator override.

## Decision

**Implement soft-limit resource budgeting with role-specific defaults and operator overrides.**

- Hard-coded minimum VRAM thresholds: training ≥ 16 GB, chat ≥ 4 GB, image-generation ≥ 8 GB.
- Proportional scaling when total available VRAM is insufficient.
- Environment-variable-based configuration for role daemons (Ollama `--num-gpu`, training `--mode=lora`).
- Cgroup v2 memory limits for training to prevent OOM kills.

## Rationale

### Soft Limits (Proposed)

- **Flexibility**: Roles negotiate resource usage dynamically at runtime.
- **Simplicity**: No kernel-level preemption logic required for Phase 1.
- **Operator Control**: `--overrides` flag allows manual tuning.

### Hard Partitioning (Alternative)

- **Isolation**: Guaranteed resources per role.
- **Complexity**: Requires cgroup v2 management and kernel cooperation.
- **Overhead**: Reserved resources may be underutilized.

## Consequences

### Positive

- Simple implementation; no kernel patches.
- Operator can override defaults for special cases.
- Graceful degradation: if VRAM is scarce, roles adapt intelligently.

### Negative

- Roles may still contend if a single role uses more than budgeted.
- Requires monitoring to detect and alert on resource exhaustion.

## Implementation Notes

- Budget logic in `internal/autotuner/colocation.go`.
- Ollama configuration in `internal/autotuner/ollama.go` (e.g., `OLLAMA_NUM_GPU`).
- Training mode selection based on available VRAM: full fine-tuning (≥16 GB), LoRA (8–15 GB), quantized (<8 GB).
- No cgroup enforcement in Phase 1; advisory limits only.
