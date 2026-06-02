# Federated Privacy

> **Scope**: Privacy guarantees for federated training jobs in the AI cluster.
> Covers the network-level no-raw-data-egress guarantee, optional differential
> privacy parameters, and how to configure DP-SGD / DP-FedAvg.

## 1. Architecture Overview

```
┌──────────────────────────────────────┐
│  Worker Node A (training namespace)  │
│  ┌─────────────────────────────────┐ │
│  │  Training Pod (role=federated-  │ │
│  │  worker)                        │ │
│  │  • Raw data stays on-disk       │ │   gradient
│  │  • Gradient updates only        │ │   updates ──► Aggregator :8443
│  │    may leave this pod           │ │
│  └─────────────────────────────────┘ │
└──────────────────────────────────────┘
         NetworkPolicy: training-gradient-egress
         (egress only on 8443/tcp)
```

Raw training data **never** leaves the node. The `training-gradient-egress`
`NetworkPolicy` in `cluster/overlays/production/network-policies.yaml` enforces
this at the kernel level: pods labelled `role: federated-worker` may only
initiate TCP connections to port 8443 (the federation aggregator). All other
outbound connections are dropped.

## 2. Differential Privacy Parameters

Differential privacy (DP) bounds how much any single training example can affect
the published model, providing a formal privacy guarantee expressed as `(ε, δ)`:

- **ε (epsilon)** — privacy loss budget. Lower ε = stronger privacy guarantee.
  Typical values: `ε = 1` (strong), `ε = 8` (moderate), `ε = 50` (weak).
- **δ (delta)** — probability of catastrophic privacy failure. Set to
  `1 / (10 × dataset_size)` or smaller; typical value `1e-5`.

### Recommended budgets

| Use case | ε | δ | Notes |
|----------|---|---|-------|
| Sensitive medical/legal data | 1.0 | 1e-6 | Highest protection |
| General user-generated content | 8.0 | 1e-5 | Good default |
| Public dataset fine-tuning | 50.0 | 1e-5 | Minimal protection |

## 3. DP-SGD Configuration (Opacus)

For PyTorch training jobs using [Opacus](https://opacus.ai):

```python
from opacus import PrivacyEngine

# Attach DP engine to the optimizer
privacy_engine = PrivacyEngine()
model, optimizer, train_loader = privacy_engine.make_private_with_epsilon(
    module=model,
    optimizer=optimizer,
    data_loader=train_loader,
    epochs=num_epochs,
    target_epsilon=8.0,    # ε
    target_delta=1e-5,     # δ
    max_grad_norm=1.0,     # gradient clipping norm
)
```

Add these environment variables to training pod specs to document the budget:

```yaml
env:
  - name: DP_EPSILON
    value: "8.0"
  - name: DP_DELTA
    value: "1e-5"
  - name: DP_MAX_GRAD_NORM
    value: "1.0"
```

## 4. DP-FedAvg Configuration

For federated averaging with differential privacy, each worker clips and noises
its gradients before sending to the aggregator:

**Per-round privacy cost** (Gaussian mechanism):

```
σ = max_grad_norm × sqrt(2 × ln(1.25/δ)) / ε_per_round
```

Where `ε_per_round = ε_total / sqrt(T × ln(1/δ))` for `T` rounds (Rényi DP
composition).

**Kubernetes pod spec snippet for a DP-FedAvg worker**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: federated-worker
  namespace: training
  labels:
    role: federated-worker      # required for NetworkPolicy
spec:
  containers:
    - name: worker
      image: ghcr.io/opd-ai/cluster/federated-worker:latest
      env:
        - name: AGGREGATOR_ENDPOINT
          value: "https://aggregator:8443"
        - name: DP_EPSILON
          value: "8.0"
        - name: DP_DELTA
          value: "1e-5"
        - name: DP_ROUNDS
          value: "100"
        - name: DP_MAX_GRAD_NORM
          value: "1.0"
```

## 5. Audit and Budget Tracking

Privacy budgets are consumed over training runs. Track cumulative ε with:

```python
epsilon, best_alpha = privacy_engine.accountant.get_privacy_spent(delta=1e-5)
print(f"Consumed: ε={epsilon:.2f}, δ=1e-5 after {step} steps")
```

Log this value to stdout; Promtail will capture it in Loki under the
`training` namespace. Alert if `ε > target_epsilon`:

```yaml
# Add to cluster/overlays/production/alerting.yaml PrometheusRule
- alert: DPBudgetExceeded
  expr: dp_epsilon_consumed > 8.0
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Federated training DP budget exceeded"
    description: "Worker {{ $labels.pod }} consumed ε={{ $value }} > budget=8.0"
```

## 6. Network-Level Enforcement Summary

The following `NetworkPolicy` resources enforce data-minimization at the
Kubernetes network layer (see
`cluster/overlays/production/network-policies.yaml`):

| Policy | Namespace | Effect |
|--------|-----------|--------|
| `default-deny-all` | `training` | Block all traffic by default |
| `allow-dns-egress` | `training` | Allow DNS resolution only |
| `training-gradient-egress` | `training` | Allow port 8443 egress from `role=federated-worker` pods only |

These policies mean that even a compromised training pod cannot exfiltrate raw
data over the network — it can only reach the aggregator endpoint on port 8443.

## 7. References

- [Opacus differential privacy library](https://opacus.ai)
- [Rényi Differential Privacy of the Gaussian Mechanism](https://arxiv.org/abs/1702.07476)
- [Deep Learning with Differential Privacy (Abadi et al., 2016)](https://arxiv.org/abs/1607.00133)
- [Advances and Open Problems in Federated Learning](https://arxiv.org/abs/1912.04977)
- [Data Governance runbooks](DATA-GOVERNANCE.md)
