# ADR 006 — Federated Learning: FedAvg vs FedProx

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The cluster supports federated fine-tuning across multiple nodes. The
aggregation algorithm choice affects convergence speed, communication cost,
and robustness to stragglers and heterogeneous data.

## Decision

**Default to FedAvg.** FedProx is supported as an opt-in alternative via
environment variable.

## Rationale

### FedAvg (default)

- Simple: weighted average of worker gradients, weighted by local dataset size.
- Communication cost: one round per communication step (gradients only).
- Works well when data is roughly IID across workers.
- Sufficient for the typical home-lab use case (2–6 nodes, similar data).

### FedProx (alternative)

FedProx adds a proximal regularisation term `(μ/2) ‖w − w_global‖²` to each
worker's local objective, which:
- Prevents workers with heterogeneous (non-IID) data from drifting too far
  from the global model.
- Allows stragglers to participate with partial work.
- Adds one hyperparameter (μ) that must be tuned.

Enable via:

```yaml
# training pod spec
env:
  - name: FEDERATED_ALGORITHM
    value: "fedprox"
  - name: FEDPROX_MU
    value: "0.01"
```

### Convergence comparison

| Algorithm | IID data | Non-IID data | Stragglers |
|-----------|----------|-------------|------------|
| FedAvg | Fast | Slow (diverges) | Poor |
| FedProx | Fast | Stable | Tolerant |

## Consequences

- `cmd/pipeline` implements FedAvg by default; `FEDERATED_ALGORITHM=fedprox`
  selects FedProx.
- The `μ` parameter should be tuned on a held-out validation set;
  start with `μ=0.01`.
- Privacy budgets (ε, δ) are computed per-round regardless of algorithm;
  see `docs/FEDERATED-PRIVACY.md`.
