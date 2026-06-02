# Architecture

> **Version**: 2026-06  
> **Status**: Living document — updated alongside PLAN.md

## Overview

```
                    ┌─────────────────────────────────────────────┐
                    │                 Tailnet (VPN)                │
                    │                                              │
  Client ──HTTPS──► │  Gateway :8080 (ai-cluster namespace)       │
                    │    │                                         │
                    │    ├── /v1/chat/completions ──► Ollama nodes │
                    │    ├── /v1/images/generations ─► SwarmUI    │
                    │    ├── /v1/videos/generations ─► Ollama     │
                    │    ├── /v1/embeddings ──────────► Ollama    │
                    │    └── /ingest, /query ─────────► RAG :8081  │
                    │                  │                           │
                    │                  └── Qdrant :6333            │
                    │                                              │
  Browser ──WSS───► │  Console :8080 (serves WASM)                │
                    └─────────────────────────────────────────────┘
```

## Data Flow

### LLM Inference

```
Client
  │  POST /v1/chat/completions {model, messages}
  ▼
Gateway (cmd/gateway)
  │  auth via API key (X-Api-Key or Authorization)
  │  select backend via Placer (inventory health check)
  ▼
Ollama :11434 (Linux/GPU node or Mac node)
  │  POST /api/chat
  ▼
Gateway  →  stream back to Client (SSE)
```

Speculative decoding: Gateway may route a prefill to a draft model on a
CPU-only node and verification to the full model on a GPU node. Draft and
full model run in separate Ollama processes on different tailnet addresses.

Hot-swap: Gateway re-reads inventory on SIGHUP; in-flight requests drain to
old backend while new requests are routed to the new backend.

### Federated Training Flow

```
Coordinator (cmd/pipeline)
  │  push dataset shards to MinIO (s3://datasets/)
  ▼
Worker nodes (training namespace, role=federated-worker)
  │  pull shard from MinIO
  │  local SGD + differential privacy clip+noise (DP-SGD / Opacus)
  │  gradient update only — raw data never leaves node
  ▼
Aggregator :8443
  │  FedAvg aggregation
  ▼
MinIO (s3://adapters/)  →  Ollama hot-load via GGUF adapter
```

Network policy `training-gradient-egress` enforces that only port 8443/TCP
leaves the training namespace (no raw data exfiltration possible).

### RAG Flow

```
Client
  │  POST /ingest {source, url, collection}
  ▼
RAG service (cmd/rag)
  │  chunk document into passages
  │  POST /api/embeddings → Ollama (nomic-embed-text)
  │  PUT /collections/{col}/points → Qdrant
  ▼
[index built]

Client
  │  POST /query {collection, query, k}
  ▼
RAG service
  │  embed query → Ollama
  │  search Qdrant (top-k cosine)
  │  prompt = retrieved chunks + query
  │  POST /api/chat → Ollama (LLM)
  ▼
Client  ←  answer
```

### Image / Video Generation Flow

```
Client
  │  POST /v1/images/generations {prompt, model, seed}
  ▼
Gateway
  │  route to SwarmUI :7801 (SDXL / Flux) or
  │         ComfyUI  :8188 (custom workflows)
  ▼
SwarmUI / ComfyUI
  │  load checkpoint from MinIO (s3://models/)
  │  generate image / video
  ▼
MinIO (s3://outputs/)  ←  store result
  │
Gateway  →  pre-signed URL or base64 in response
```

### Console (Ebitengine WASM) Flow

```
Browser
  │  GET /  →  HTTP server (cmd/console / Go net/http)
  │             serves index.html + wasm_exec.js + console.wasm
  │
  │  WebAssembly instantiate console.wasm
  │  Ebitengine game loop (60 fps, requestAnimationFrame)
  │
  │  WS /ws  ←→  console server
  │              proxies to Gateway API
  │
  ▼
Rendered UI (canvas 2D) — no DOM, no JS framework
```

The WASM binary is compiled with `GOOS=js GOARCH=wasm` from `cmd/console-wasm`.
Ebitengine renders to an HTML `<canvas>` element. All API calls go through the
WebSocket proxy to avoid CORS issues. The accessibility tradeoff (canvas vs DOM
elements) is documented in ADR 007.

### Failure Modes

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Ollama node down | Gateway health check (30 s) | Route to next healthy backend |
| Qdrant unavailable | RAG /healthz → 503 | RAG returns error; no silent data loss |
| MinIO unavailable | Gateway 503 on image/video routes | Client retry |
| k3s etcd quorum lost | `cmd/status` diff alert | Restore from etcd snapshot (runbook) |
| Federated round timeout | Aggregator deadline | Skip round, preserve last checkpoint |
| WASM canvas freeze | Browser tab reload | Console state is stateless — reconnects |

## Package Map

| Package | Purpose |
|---------|---------|
| `cmd/gateway` | OpenAI-compatible HTTP router, auth, health checks, metrics |
| `cmd/rag` | Ingest, embed, query, /metrics endpoint |
| `cmd/console` | WASM host: serves HTML, wasm_exec.js, WebSocket proxy |
| `cmd/console-wasm` | Ebitengine WASM binary (compiled js/wasm) |
| `cmd/cluster-bootstrap` | k3s install, node provisioning |
| `cmd/cluster-join` | Add a new node to a running cluster |
| `cmd/drain` | Cordon, drain, and remove a node |
| `cmd/status` | Diff declared vs actual cluster state |
| `cmd/powermetrics-exporter` | macOS Apple Silicon Prometheus exporter |
| `internal/sshutil` | SSH dial helpers used by bootstrap/join/drain |
| `internal/tracing` | Shared OpenTelemetry init / middleware |
| `cluster/overlays/production/` | Kustomize overlay: all k8s manifests |
| `cluster/flux-system/` | Flux GitOps components |
| `python/` | PyTorch training environment (uv-managed) |

## Dependency Versions

See `docs/MODEL-REBUILD-GUARANTEE.md` for the full version matrix.
