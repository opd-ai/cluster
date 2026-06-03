# Architecture

> **Version**: 2026-06  
> **Status**: Living document — updated alongside PLAN.md

## Overview

The cluster uses a **zero-configuration architecture** by default. Nodes automatically discover each other via UDP multicast beacons on `239.77.0.1:9977`. The gateway joins the discovery multicast group and routes requests to discovered backends without requiring manual inventory configuration.

```
                    ┌─────────────────────────────────────────────┐
                    │                 Tailnet (VPN)                │
                    │                                              │
                    │  ┌────────────────────────────────────────┐ │
                    │  │  UDP Multicast Discovery (239.77.0.1)  │ │
                    │  │  • node-agent broadcasts every 10s     │ │
                    │  │  • gateway/peers auto-discover nodes   │ │
                    │  └────────────────────────────────────────┘ │
                    │                                              │
  Client ──HTTPS──► │  Gateway :8080 (ai-cluster namespace)       │
                    │    │                                         │
                    │    ├── /v1/chat/completions ──► Ollama nodes │
                    │    ├── /v1/images/generations ─► SwarmUI    │
                    │    ├── /v1/videos/generations ─► SwarmUI    │
                    │    ├── /v1/embeddings ──────────► Ollama    │
                    │    └── /ingest, /query ─────────► RAG :8081  │
                    │                  │                           │
                    │                  └── Qdrant :6333            │
                    │                                              │
  Browser ──WSS───► │  Console :8080 (serves WASM)                │
                    └─────────────────────────────────────────────┘
```

## Node Discovery Flow

Zero-configuration deployment uses the following discovery mechanism:

```
Node A (node-agent)
  │  Start node-agent with --roles and --address
  │  UDP beacon every 10s → 239.77.0.1:9977
  │    Payload: {hostname, roles, address, port, seq}
  ▼
Multicast Group (239.77.0.1:9977)
  │  Gateway and other node-agents join group
  ▼
Gateway / Peer Nodes
  │  Receive beacon, deduplicate by (address, seq)
  │  HTTP GET /api/v1/info → Node A (full capability metadata)
  │  internal/discovery/reconciler.go merges into inventory
  ▼
Node registered in lb.BackendRegistry
  │  Available for request routing
```

**Fallback:** If link-local multicast is filtered (corporate networks), discovery falls back to tailnet unicast.

## Data Flow

### LLM Inference (Zero-Conf Path)

```
Client
  │  POST /v1/chat/completions {model, messages}
  ▼
Gateway (cmd/gateway) [--discovery=true by default]
  │  auth via API key (Authorization: Bearer)
  │  select backend via lb.BackendRegistry
  │    → auto-discovered backends from multicast
  │    → or manually configured inventory
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
old backend while new requests are routed to the new backend. Auto-discovered
nodes are registered immediately without SIGHUP.

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
  │             serves index.html + wasm_exec.js + main.wasm
  │
  │  WebAssembly instantiate main.wasm
  │  Ebitengine game loop (60 fps, requestAnimationFrame)
  │
  │  WS /api/ws  ←→  console server
  │              proxies to Gateway API
  │
  ▼
Rendered UI (canvas 2D) — no DOM, no JS framework
```

The WASM binary is compiled with `GOOS=js GOARCH=wasm` from `cmd/console-wasm`.
Ebitengine renders to an HTML `<canvas>` element. All API calls go through the
WebSocket proxy to avoid CORS issues. The accessibility tradeoff (canvas vs DOM
elements) is documented in ADR 007.
<!-- REVIEW: Makefile builds web/console.wasm (WASM_OUT), but cmd/console serves
/main.wasm (cmd/console/main.go:136). The build artifact and served filename
still differ; confirm the canonical name and align both paths. -->

### Console Bootstrap Assets

The console server (`cmd/console`) leaves `/`, `/index.html`, `/main.wasm`, and
`/wasm_exec.js` publicly accessible so the login page can load; every other
static asset and API route (including `/api/ws`, authenticated via the `token`
query parameter) requires a session token.

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
