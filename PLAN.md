# Auto-Discovery and Zero-Configuration Deployment Plan

## Summary

This plan extends the `opd-ai/cluster` monorepo to deliver single-command, zero-config
node deployment with automatic LAN peer discovery, intelligent load balancing, cross-node
WebUI observability, and generative pipeline chaining. A central design constraint is that
**a single physical host must be able to run any combination of node types simultaneously**
(`chat`, `image-generation`, `training`); the inventory `Node` schema is extended from a
single `role` string to a `roles` list, and resource budgeting logic partitions GPU/VRAM,
CPU, and RAM across co-located roles at deploy time. Manual `cluster/inventory.yaml` editing
remains fully supported; auto-discovery is additive and reconciles into the same schema.

---

## Assumptions & Open Questions

| # | Assumption / Question | Impact |
|---|---|---|
| A1 | Ollama continues to be the primary inference runtime; SwarmUI for images | Auto-tune logic is Ollama-specific |
| A2 | Tailscale/tailnet is already present on all nodes (from ADR-003) | Discovery can use tailnet multicast OR link-local mDNS |
| A3 | k3s nodes run Linux; macOS/Ollama nodes are non-k3s tailnet peers | Deploy tool must branch on `os` |
| A4 | `golang.org/x/net` (already in `go.sum` as transitive dep) exposes `net.UDPConn`; stdlib `net` is sufficient for UDP multicast | No new direct dependency needed for discovery |
| A5 | Training role requires VRAM ≥ 16 GB; chat requires ≥ 4 GB; image-generation ≥ 8 GB | Hard-coded thresholds; operator-overridable via flag |
| A6 | Port namespace conflicts on a single host are resolved by a fixed port-per-role table (see §Coexistence) | Two hosts of the same role use the same port; intra-host roles use different ports |
| **Q1** | Should the node-agent expose a gRPC or HTTP/JSON API? | **Proposed:** HTTP/JSON only (keeps stdlib `net/http`; avoids new grpc dependency on agent binary) |
| **Q2** | Should discovery be mDNS/DNS-SD compliant or a custom UDP beacon? | **Proposed:** Custom UDP multicast beacon on `239.77.0.1:9977` — avoids `github.com/grandcat/zeroconf`; falls back to tailnet broadcast if link-local multicast is filtered |
| **Q3** | Resource contention: hard partitioning vs. best-effort cgroups? | **Proposed:** soft limits expressed as Ollama `--num-gpu` / `--num-ctx` flags + Linux cgroup v2 memory limits for training; no hard preemption in P1 |
| **Q4** | Is `cmd/cluster-bootstrap` the right vehicle for the deploy command or should a new `cmd/node-deploy` be separate? | **Proposed:** New `cmd/node-deploy`; bootstrap stays for k3s cluster control-plane setup |

---

## Architecture Overview

### Component Map

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Physical Host A  (multi-role example: chat + image-generation)          │
│                                                                         │
│  ┌────────────────┐   ┌──────────────────┐   ┌──────────────────────┐  │
│  │  ollama daemon │   │  swarmui daemon  │   │  cmd/node-agent      │  │
│  │  port 11434    │   │  port 7860       │   │  port 9977 (HTTP)    │  │
│  │  role: chat    │   │  role: image-gen │   │  + UDP beacon 9977   │  │
│  │  VRAM budget:  │   │  VRAM budget:    │   │  /api/v1/info        │  │
│  │   60% total    │   │   40% total      │   │  /api/v1/health      │  │
│  └────────────────┘   └──────────────────┘   │  /api/v1/metrics     │  │
│           │                   │              │  /api/v1/pipeline    │  │
│           └──────────┬────────┘              └──────────────────────┘  │
│                      │ managed by node-agent                           │
└─────────────────────────────────────────────────────────────────────────┘
         ▲ UDP multicast beacon (239.77.0.1:9977)
         │
┌──────────────────────────────────────────────────────────────────────┐
│ cmd/gateway  (updated)                                                │
│  internal/lb.Picker (weighted-round-robin + latency-aware)           │
│  routes by role+model; knows host A serves BOTH chat & image-gen     │
└──────────────────────────────────────────────────────────────────────┘
         │
┌──────────────────────────────────────────────────────────────────────┐
│ cmd/console (server)  +  cmd/console-wasm (client)                   │
│  internal/uiapi: ClusterState.Nodes[]  NodeState.Roles []string      │
│  new: AggregateMetrics, PipelineState, GenerationEvent               │
└──────────────────────────────────────────────────────────────────────┘
```

### Data-Flow Summary

1. **Deploy**: operator runs `node-deploy --roles chat,image-generation` on a host.
   - `internal/autotuner` reads local hardware (reuses probe logic from `cmd/cluster-probe`).
   - Derives Ollama/SwarmUI config; writes systemd/launchd unit files.
   - Starts `cmd/node-agent` as a supervisor process.

2. **Discovery**: `cmd/node-agent` emits UDP beacon every 10 s; `cmd/gateway` and peer
   node-agents listen. On first contact, HTTP `GET /api/v1/info` fetches full capability
   record. Gateway reconciles into its in-memory backend list AND optionally patches
   `cluster/inventory.yaml`.

3. **Load Balancing**: `cmd/gateway` calls `internal/lb.Picker.Pick(role, model, hint)`
   which selects a backend using weighted-round-robin with latency EMA and queue-depth
   signals polled from `/api/v1/metrics`.

4. **WebUI**: `cmd/console` polls all node-agent `/api/v1/metrics` endpoints; aggregated
   `AggregateMetrics` is pushed via WebSocket to all connected `cmd/console-wasm` clients.

5. **Pipelines**: client posts a `PipelineSpec` to `POST /v1/pipelines` on the gateway.
   Gateway executes stages serially: stage output is forwarded as the next stage's input
   via `POST /api/v1/pipeline/submit` on the target node-agent. Results stream back as
   SSE or via a job poll endpoint.

---

## Phased Implementation Plan

### P0 — Schema & Backward-Compatibility Foundation

**Goal:** Extend the inventory `Node` schema to support multiple roles per host without
breaking existing single-role YAML files. All downstream consumers updated to read the
new `roles` list.

**Affected paths:**

| Path | Change |
|---|---|
| `internal/inventory/node.go` *(new package)* | Canonical `Node` struct with `Roles []string`; backward-compat `Role string` read-only alias; `ServiceBinding` type |
| `cluster/inventory.yaml` | Add `roles` list; keep `role` for one release cycle |
| `cmd/cluster-probe/main.go` | Import `internal/inventory`; emit `roles` field |
| `cmd/cluster-bootstrap/main.go` | Import `internal/inventory`; read `roles` |
| `cmd/gateway/main.go` | Import `internal/inventory`; replace inline `Node` struct |
| `cmd/status/main.go` | Import `internal/inventory` |
| `internal/uiapi/types.go` | `NodeState.Roles []string`; deprecate `NodeState.Role` |
| `docs/adr/008-multi-role-colocation.md` *(new)* | ADR stub |

**Dependencies:** None.

---

### P1 — `cmd/node-deploy`: Single-Command Zero-Config Deployment

**Goal:** `node-deploy --roles chat,image-generation` on a fresh host enumerates local
hardware, derives per-role resource budgets, installs/configures software, and starts
`cmd/node-agent`.

**Affected paths:**

| Path | Change |
|---|---|
| `cmd/node-deploy/main.go` *(new)* | Entry point; parses `--roles`, invokes autotuner, writes config, installs services |
| `internal/autotuner/autotuner.go` *(new package)* | Hardware → config derivation; reuses probe logic |
| `internal/autotuner/ollama.go` *(new)* | Generates Ollama model file + env vars |
| `internal/autotuner/swarmui.go` *(new)* | Generates SwarmUI launch config |
| `internal/autotuner/training.go` *(new)* | Generates k8s-trainer / fine-tune config |
| `internal/autotuner/colocation.go` *(new)* | Multi-role VRAM / RAM budget split logic |
| `internal/serviceinstall/linux.go` *(new)* | systemd unit file generation + `systemctl enable/start` |
| `internal/serviceinstall/darwin.go` *(new)* | launchd plist generation + `launchctl load` |
| `Makefile` | Add `deploy` target: `$(GO) run ./cmd/node-deploy $(ROLES)` |
| `docs/adr/009-discovery-protocol.md` *(new)* | ADR stub |
| `docs/adr/010-auto-tuning-budgeting.md` *(new)* | ADR stub |

**Auto-tuning logic per role** (signals from local `/proc`, `nvidia-smi`, `sysctl`):

| Role | Key signals | Derived settings |
|---|---|---|
| `chat` | `vram_gb`, `ram_gb`, `num_cpu` | Ollama `--num-gpu`, `--num-ctx` (VRAM budget / 2 GiB per ctx slot), concurrency limit |
| `image-generation` | `vram_gb`, `accelerator` | SwarmUI `--max-gpu-layers`, batch size (1 per 8 GiB VRAM) |
| `training` | `vram_gb` (must ≥ 16 GB), `ram_gb`, `disk_gb` | LoRA rank, batch size, gradient checkpointing on/off |

**Multi-role resource split** (`internal/autotuner/colocation.go`):

- Default policy: divide total VRAM equally across GPU-bound roles (chat + image-gen + training each get `vram_gb / N` GiB).
- Operator override: `--vram-split chat=60,image-generation=40` (percentages, must sum ≤ 100).
- CPU-only roles (embeddings, RAG) get no VRAM allocation; their concurrency limit = `ram_gb / 8`.
- Training has lowest default priority; if co-located it gets the floor after inference roles.
- Minimum VRAM thresholds are hard-enforced; `node-deploy` exits with a diagnostic if a role cannot meet its minimum on the available budget.

**Per-role port allocation** (fixed table, no collisions on a single host):

| Role | Process | Default port |
|---|---|---|
| `chat` | Ollama | 11434 |
| `image-generation` | SwarmUI | 7860 |
| `training` | k8s-trainer HTTP | 7861 |
| `embeddings` | Ollama (shared with chat if co-located) | 11434 |
| `node-agent` | node-agent HTTP | 9977 |

When `chat` and `embeddings` share a host, they share the Ollama instance (one port).
All ports are configurable via `--port-<role>=N` flags.

**Dependencies:** P0.

---

### P2 — `cmd/node-agent` + `internal/discovery`: Peer Discovery

**Goal:** Each deployed host runs a long-lived `node-agent` that (a) manages local role
processes, (b) broadcasts a UDP beacon so peers and the gateway auto-discover it,
(c) serves a local HTTP API, and (d) reconciles discovered peers into
`cluster/inventory.yaml`.

**Affected paths:**

| Path | Change |
|---|---|
| `cmd/node-agent/main.go` *(new)* | Supervisor + HTTP server + discovery participation |
| `internal/discovery/beacon.go` *(new package)* | UDP multicast sender (239.77.0.1:9977, stdlib `net`) |
| `internal/discovery/listener.go` *(new)* | UDP multicast receiver; deduplicates by `(address, roles)` |
| `internal/discovery/reconciler.go` *(new)* | Merges discovered nodes into inventory YAML (atomic write) |
| `internal/nodeapi/types.go` *(new package)* | Wire types for node-agent HTTP API |
| `Makefile` | Add `agent` target; add to `up` sequence |

**UDP Beacon wire format** (JSON, ≤ 512 bytes):

```
{ "v": 1, "hostname": "...", "address": "...",
  "roles": ["chat","image-generation"],
  "services": [{"role":"chat","port":11434},{"role":"image-generation","port":7860},{"role":"node-agent","port":9977}],
  "arch": "amd64", "os": "linux",
  "vram_gb": 24, "ram_gb": 64,
  "seq": 42 }
```

**Node-agent HTTP API** (`internal/nodeapi/types.go`):

| Endpoint | Method | Response type | Description |
|---|---|---|---|
| `/api/v1/info` | GET | `NodeInfo` | Static hardware + role capabilities |
| `/api/v1/health` | GET | `HealthReport` | Per-role liveness (process up + model loaded) |
| `/api/v1/metrics` | GET | `NodeMetrics` (extended) | VRAM used/total per role, CPU%, queue depth |
| `/api/v1/peers` | GET | `[]PeerRecord` | Peers this agent knows about |
| `/api/v1/pipeline/submit` | POST | `PipelineAck` | Accept pipeline stage input; returns job ID |
| `/api/v1/pipeline/result/{id}` | GET | `PipelineResult` | Poll or stream stage output |

`NodeInfo` fields: `hostname`, `address`, `roles []string`, `services []ServiceBinding`,
`arch`, `os`, `accelerator`, `vram_gb`, `ram_gb`, `disk_gb`, `vram_budget map[string]int`.

**Discovery reconciliation with existing inventory:**
- If a host with the same `address` already exists in `cluster/inventory.yaml`, the
  reconciler merges its `roles` list (union) and updates `services`; it does NOT overwrite
  manually set fields (`ssh_user`, `labels`).
- If no entry exists, it appends a new node record.
- Reconciliation writes are atomic (write to temp file, `os.Rename`).
- Flag `--no-reconcile` on node-agent disables YAML mutation (read-only discovery).

**Backward compatibility:** The gateway's `-inventory` flag path still works; gateway also
optionally listens for UDP beacons directly when `-discovery=true` is set.

**Dependencies:** P0, P1.

---

### P3 — `internal/lb`: Improved Load Balancing in `cmd/gateway`

**Goal:** Replace the current naive round-robin / sticky-session picker with a pluggable
strategy that accounts for queue depth, latency, and per-role routing.

**Affected paths:**

| Path | Change |
|---|---|
| `internal/lb/picker.go` *(new package)* | `Picker` interface + `WeightedRoundRobin`, `LeastQueue`, `LatencyEWMA` implementations |
| `internal/lb/registry.go` *(new)* | `BackendRegistry`: maps `(role, model)` → `[]BackendRecord`; updated by discovery events |
| `cmd/gateway/main.go` | Replace inline `pickBackend` with `lb.Picker`; add `-lb-strategy` flag |
| `cmd/gateway/main.go` | `discoverBackends` replaced by `internal/lb.BackendRegistry` populated from inventory + UDP listener |

**`BackendRecord`** (replaces `Backend`):

```
type BackendRecord struct {
    Address      string
    Roles        []string
    Services     []ServiceBinding   // per-role ports
    Models       []string
    Healthy      bool
    QueueDepth   int
    LatencyEMAms float64
    LastSeen     time.Time
}
```

**Strategy selection** (flag `-lb-strategy=weighted-rr|least-queue|latency-ewma`):

| Strategy | When to use | Signal polled |
|---|---|---|
| `weighted-rr` | Default; no metric overhead | none (equal weights) |
| `least-queue` | Heterogeneous hardware | `/api/v1/metrics` queue_depth |
| `latency-ewma` | Latency-sensitive workloads | per-request round-trip time |

**Routing multi-role hosts:** When the gateway routes an image-generation request to a
host that also runs chat, it uses the `services` table to send to port 7860, not 11434.
This requires `BackendRecord.Services` to be populated (from discovery or inventory).

**Dependencies:** P0, P2.

---

### P4 — `internal/uiapi` + WebUI Extensions: Cross-Node Observability

**Goal:** Extend `internal/uiapi/types.go` and the console server/client so operators
can see live metrics, generation outputs, and job states from ALL nodes in one view.

**Affected paths:**

| Path | Change |
|---|---|
| `internal/uiapi/types.go` | Add types listed below |
| `cmd/console/main.go` | Add aggregation loop over all node-agent `/api/v1/metrics` |
| `cmd/console/ws.go` | Broadcast new message types via WebSocket |
| `cmd/console-wasm/scene_cluster.go` | Update to render `NodeState.Roles []string`, per-role VRAM bars |
| `cmd/console-wasm/scene_imagestudio.go` | Subscribe to `MsgGenerationEvent` for live preview aggregation |
| `cmd/console-wasm/scene_training.go` | Subscribe to `MsgTrainingMetrics` (already exists); extend for multi-node |

**New / updated types in `internal/uiapi/types.go`:**

| Type | Purpose |
|---|---|
| `NodeState.Roles []string` | Replaces single `Role`; backward compat: marshal both |
| `NodeState.Services []ServiceBinding` | Port-per-role info shown in UI |
| `NodeState.VRAMBudget map[string]int64` | Per-role VRAM allocation in MB |
| `NodeState.QueueDepth map[string]int` | Per-role pending request count |
| `AggregateMetrics` | Cluster-wide rollup: total VRAM, CPU, queue depth |
| `GenerationEvent` | Unified type for chat tokens, image previews, video frames from any node |
| `PipelineState` | Tracks a multi-stage pipeline job (see P5) |
| `MsgAggregateMetrics MessageType = "aggregate_metrics"` | New WS message type |
| `MsgGenerationEvent MessageType = "generation_event"` | New WS message type |
| `MsgPipelineState MessageType = "pipeline_state"` | New WS message type |

**Console aggregation loop** (`cmd/console/main.go`):
- Every 5 s, poll all known node-agents' `/api/v1/metrics`.
- Aggregate into `AggregateMetrics`; push to all WebSocket clients.
- Node-agents that fail to respond within 2 s are marked unhealthy in `NodeState.Healthy`.

**Dependencies:** P0, P2, P3.

---

### P5 — `internal/pipeline`: Cross-Node Generative Pipelines

**Goal:** Clients post a `PipelineSpec` to the gateway; the gateway executes stages
serially, routing each stage output as the next stage's input, across nodes.

**Affected paths:**

| Path | Change |
|---|---|
| `internal/pipeline/spec.go` *(new package)* | `PipelineSpec`, `Stage`, `StageResult` types |
| `internal/pipeline/executor.go` *(new)* | Stage execution loop; calls node-agent `/api/v1/pipeline/submit` |
| `cmd/gateway/main.go` | Add `POST /v1/pipelines` route; delegate to `pipeline.Executor` |
| `cmd/node-agent/main.go` | Wire `/api/v1/pipeline/submit` to local role handler |
| `internal/uiapi/types.go` | `PipelineState` (added in P4) |
| `docs/adr/011-pipeline-api.md` *(new)* | ADR stub |

**`PipelineSpec`** wire format (JSON, sent to `POST /v1/pipelines`):

```
{ "stages": [
    { "role": "chat",             "input": {"prompt": "..."}, "model": "llama3" },
    { "role": "image-generation", "input": {"use_prev_output": true, "size": "1024x1024"} }
  ],
  "stream": true
}
```

`use_prev_output: true` instructs the executor to pass the previous stage's text/image
output as the `prompt` / `init_image` field of the next stage's input.

**Intra-node pipeline hand-off** (`POST /api/v1/pipeline/submit` on node-agent):

```
Request:  { "job_id": "...", "role": "image-generation",
            "input": { "prompt": "<text from chat stage>", "size": "1024x1024" } }
Response: { "job_id": "...", "status": "accepted" }
```

Results polled via `GET /api/v1/pipeline/result/{job_id}` (returns `PipelineResult`
with `status`, `output_text`, `output_image_url`, `output_video_url`).

**Dependencies:** P0, P2, P3, P4.

---

## Task Checklist

### Phase 0 — Schema & Backward Compatibility

- [x] Create `internal/inventory/node.go` with `Node{Roles []string, Services []ServiceBinding, VRAMBudget map[string]int}` and backward-compat `Role()` accessor
- [x] Add `ServiceBinding{Role, Port string}` to `internal/inventory/node.go`
- [x] Update `cluster/inventory.yaml` to add `roles` list alongside existing `role` fields
- [x] Update `cmd/cluster-probe/main.go` to import `internal/inventory` and emit `roles`
- [x] Update `cmd/cluster-bootstrap/main.go` to use `internal/inventory.Node`
- [x] Update `cmd/gateway/main.go` inline `Backend` struct to load `Roles`/`Services` from inventory via `internal/inventory`
- [x] Update `cmd/status/main.go` to use `internal/inventory`
- [x] Update `internal/uiapi/types.go`: add `Roles []string` to `NodeState`; keep `Role` marshaled for one release
- [x] Add `ServiceBinding` and `VRAMBudget` fields to `internal/uiapi.NodeState`
- [x] Write `docs/adr/008-multi-role-colocation.md` ADR stub (status: Proposed)
- [x] Run `make lint` and `make test` — no regressions

### Phase 1 — node-deploy + autotuner

- [x] Create `internal/autotuner/autotuner.go`: `HardwareProfile` struct; `Probe() HardwareProfile` (reuses SSH-free local-probe logic from `cmd/cluster-probe`)
- [x] Create `internal/autotuner/colocation.go`: `BudgetSplit(roles []string, hw HardwareProfile, overrides map[string]int) map[string]ResourceBudget`
- [x] Create `internal/autotuner/ollama.go`: `OllamaConfig(role string, budget ResourceBudget) OllamaEnv`
- [x] Create `internal/autotuner/swarmui.go`: `SwarmUIConfig(budget ResourceBudget) SwarmUIArgs`
- [x] Create `internal/autotuner/training.go`: `TrainingConfig(budget ResourceBudget) TrainingEnv`
- [x] Create `internal/serviceinstall/linux.go`: systemd unit file writer for node-agent + role daemons
- [x] Create `internal/serviceinstall/darwin.go`: launchd plist writer
- [x] Create `cmd/node-deploy/main.go`: parse `--roles`, call autotuner, call serviceinstall, write `cluster/inventory.yaml` entry
- [x] Add `deploy` Makefile target: `go run ./cmd/node-deploy --roles $(ROLES)`
- [x] Write `docs/adr/009-discovery-protocol.md` ADR stub (UDP beacon vs. mDNS)
- [x] Write `docs/adr/010-auto-tuning-budgeting.md` ADR stub
- [x] Run `make lint` and `make test` — no regressions
- [x] Manual smoke test: `node-deploy --roles chat` on a Linux dev box; verify Ollama unit file is generated with correct `--num-gpu`

### Phase 2 — node-agent + discovery

- [x] Create `internal/nodeapi/types.go`: `NodeInfo`, `HealthReport`, `NodeMetricsExt`, `PeerRecord`, `PipelineAck`, `PipelineResult`, `BeaconMessage`
- [x] Create `internal/discovery/beacon.go`: UDP multicast sender on `239.77.0.1:9977` using stdlib `net`
- [x] Create `internal/discovery/listener.go`: UDP multicast receiver; emits `BeaconMessage` on a channel
- [x] Create `internal/discovery/reconciler.go`: merge discovered nodes into `cluster/inventory.yaml` atomically
- [x] Create `cmd/node-agent/main.go`: starts beacon, HTTP server, process supervisor (one goroutine per role daemon)
- [x] Implement `GET /api/v1/info` on node-agent (returns `NodeInfo`)
- [x] Implement `GET /api/v1/health` on node-agent (returns `HealthReport`)
- [x] Implement `GET /api/v1/metrics` on node-agent (returns `NodeMetricsExt`)
- [x] Implement `GET /api/v1/peers` on node-agent
- [x] Add `--no-reconcile` flag to node-agent
- [ ] Update gateway to optionally join discovery multicast group (`-discovery=true` flag)
- [ ] Add `agent` Makefile target: `go run ./cmd/node-agent --roles $(ROLES)`
- [x] Run `make lint` and `make test`
- [ ] Integration test: two node-agent instances on same LAN discover each other within 30 s

### Phase 3 — lb package + gateway routing

- [x] Create `internal/lb/picker.go`: `Picker` interface; `WeightedRoundRobin` implementation
- [x] Create `internal/lb/least_queue.go`: `LeastQueue` implementation (polls `QueueDepth` from `BackendRecord`)
- [x] Create `internal/lb/latency_ewma.go`: `LatencyEWMA` implementation
- [x] Create `internal/lb/registry.go`: `BackendRegistry` with `Register`, `Deregister`, `Pick(role, model, hint)` methods
- [ ] Update `cmd/gateway/main.go`: replace `pickBackend` / `discoverBackends` with `lb.BackendRegistry` + `lb.Picker`
- [ ] Add `-lb-strategy` flag to gateway
- [ ] Ensure multi-role routing: gateway uses `ServiceBinding.Port` to route image-gen to port 7860 on a host also serving chat on 11434
- [x] Run `make lint` and `make test`
- [ ] Load test: simulate 3 backends, one with queue=10; verify `least-queue` strategy routes away from it

### Phase 4 — uiapi extensions + console

- [x] Add `AggregateMetrics` type to `internal/uiapi/types.go`
- [x] Add `GenerationEvent` type to `internal/uiapi/types.go`
- [x] Add `PipelineState` type to `internal/uiapi/types.go`
- [x] Add `MsgAggregateMetrics`, `MsgGenerationEvent`, `MsgPipelineState` constants
- [ ] Update `cmd/console/main.go`: aggregation loop polling all known node-agents every 5 s
- [ ] Update `cmd/console/ws.go`: push `AggregateMetrics` and `GenerationEvent` messages
- [ ] Update `cmd/console-wasm/scene_cluster.go`: render `Roles []string` per node; show per-role VRAM bar
- [ ] Update `cmd/console-wasm/scene_imagestudio.go`: subscribe to `MsgGenerationEvent` for cross-node previews
- [ ] Rebuild WASM: `make console-wasm`
- [x] Run `make lint` and `make test`

### Phase 5 — pipeline package + gateway endpoint

- [x] Create `internal/pipeline/spec.go`: `PipelineSpec`, `Stage`, `StageResult` types
- [x] Create `internal/pipeline/executor.go`: serial stage execution loop with per-stage timeout
- [x] Add `POST /v1/pipelines` route to `cmd/gateway/main.go`
- [x] Implement `POST /api/v1/pipeline/submit` on `cmd/node-agent/main.go`
- [x] Implement `GET /api/v1/pipeline/result/{id}` on node-agent
- [ ] Add `PipelineState` WebSocket push in `cmd/console/ws.go` (re-uses P4 type)
- [x] Write `docs/adr/011-pipeline-api.md` ADR stub
- [x] Run `make lint` and `make test`
- [ ] End-to-end test: `POST /v1/pipelines` with chat→image stages; verify image URL in response

---

## Success Criteria

| # | Criterion | Measurable condition |
|---|---|---|
| SC1 | Single-command multi-role deploy | `node-deploy --roles chat,image-generation` on a fresh Linux host with an NVIDIA GPU installs Ollama (chat) + SwarmUI (image-gen), writes valid systemd units, and both services respond to health checks — **zero manual config edits** |
| SC2 | Auto-tuning correctness | On a 24 GiB VRAM host with `--roles chat,image-generation`, the derived budgets sum to ≤ 24 GiB and each role meets its minimum threshold (chat ≥ 4 GiB, image-gen ≥ 8 GiB) |
| SC3 | Peer discovery latency | A second `node-agent` started on the same LAN appears in `GET /api/v1/peers` on the first agent within 30 s without any manual config change |
| SC4 | Inventory reconciliation | `cluster/inventory.yaml` is updated with the new node record within 60 s of `node-agent` start; existing manually-set `labels` and `ssh_user` are preserved |
| SC5 | Gateway multi-role routing | A request to `POST /v1/images/generations` is routed to port 7860 on a host that also serves `POST /v1/chat/completions` on port 11434 |
| SC6 | Load-balancing eviction | With `--lb-strategy=least-queue`, a backend with queue depth ≥ 5 receives no new requests when a zero-queue backend is available |
| SC7 | Cross-node WebUI | The console WASM client connected to node A shows live VRAM usage and generation previews from node B without page reload |
| SC8 | Pipeline execution | `POST /v1/pipelines` with a 2-stage `chat→image-generation` spec returns an image URL; the prompt flowing into stage 2 is the text output of stage 1 |
| SC9 | Backward compatibility | Existing `cluster/inventory.yaml` with only `role: worker` (singular) loads without error in all updated tools; no existing `make` targets break |
| SC10 | Manual inventory still works | Gateway starts with `-inventory cluster/inventory.yaml` and no `-discovery` flag; it serves requests correctly from the hand-edited file |

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **UDP multicast blocked on tailnet/cloud** | High | Node-agent falls back to unicast "announce to known peers" mode using the existing inventory as a seed list; document `-discovery-mode=unicast` flag |
| **VRAM budget underestimation** | Medium | Auto-tuner reserves 1 GiB OS headroom per GPU; operator override via `--vram-split`; hard minimum thresholds prevent silent OOM |
| **Training pre-empts inference on co-located host** | High | Training job starts with Linux cgroup memory soft limit = its VRAM budget; training binary respects `CUDA_VISIBLE_DEVICES` per-role; gateway health probe detects VRAM pressure and marks chat backend degraded |
| **Inventory YAML corruption on concurrent writes** | Medium | `internal/discovery/reconciler.go` uses `os.CreateTemp` + `os.Rename` (atomic on POSIX); single-writer mutex guards file operations |
| **Port conflicts when operator manually assigns overlapping ports** | Low | `node-deploy` validates port availability on startup; exits with a clear error if a port is in use |
| **`discoverBackends` naive string-scan in gateway breaks with multi-role YAML** | High | P0 replaces this with `internal/inventory` proper YAML parsing via `gopkg.in/yaml.v3` |
| **New `cmd/node-agent` binary adds deployment complexity** | Medium | `node-deploy` installs and starts `node-agent` automatically; `make agent` provides a dev shortcut |
| **mDNS alternative dependency** | Low | Plan avoids new direct dep by using stdlib UDP multicast; if DNS-SD interop is later required, `golang.org/x/net/dns/dnsmessage` (already transitive) can be used |
| **WASM binary size growth from new uiapi types** | Low | New types are small structs; no binary size concern expected |
| **Pipeline stage timeout / partial failure** | Medium | `pipeline.Executor` applies per-stage timeout (configurable, default 5 min); partial results are returned with `status: partial`; gateway returns HTTP 206 |

---

## ADR Stubs to Create

| File | Title | Status |
|---|---|---|
| `docs/adr/008-multi-role-colocation.md` | Multi-role colocation model: single host, multiple node types | Proposed |
| `docs/adr/009-discovery-protocol.md` | Peer discovery: UDP multicast beacon vs. mDNS/DNS-SD | Proposed |
| `docs/adr/010-auto-tuning-budgeting.md` | Auto-tuning: hardware-signal-to-config derivation and VRAM budget split policy | Proposed |
| `docs/adr/011-pipeline-api.md` | Pipeline hand-off API: HTTP push vs. message queue | Proposed |

Each stub should follow the format established in `docs/adr/001-control-plane.md`:
`# ADR NNN — Title`, **Status**, **Date**, **Context**, **Decision**, **Rationale**,
**Consequences**.

---

## Coexistence Requirement — Summary Table

| Concern | Solution |
|---|---|
| **(a) Inventory schema** | `Node.Roles []string` replaces `Node.Role string`; backward-compat accessor reads singular `role` if `roles` absent |
| **(b) Resource partitioning** | `internal/autotuner/colocation.go` splits VRAM/RAM by role count with operator-override; training gets floor; minimums enforced |
| **(c) Per-role isolation** | Each role runs as a separate OS process/service on a distinct port; systemd/launchd unit per process; optional cgroup slice per role |
| **(d) Auto-tuning with N roles** | `BudgetSplit` divides `vram_gb` equally by default; flags override; minimum-threshold check fails fast |
| **(e) Discovery / gateway / WebUI** | `BeaconMessage.roles` is a list; `BackendRecord.Services` maps role→port; gateway routes per-role; WebUI shows `Roles []string` + per-role VRAM bars |
| **Default colocation policy** | Equal VRAM split; training lowest priority; operator overrides via `--vram-split` and `--port-<role>` flags to `node-deploy` |
