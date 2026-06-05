# Implementation Gaps — 2026-06-05

This report lists the currently open implementation gaps in the project. It complements `AUDIT.md` with detailed remediation paths for each gap.

---

## Health Metrics Return Placeholder Values (TODO)

- **Intended Behavior**: `cmd/node-agent` exposes real-time metrics via `/api/v1/metrics` including:
  - Per-role VRAM usage (MB and percentage)
  - Per-role queue depth (pending inference requests)
  - Node-wide CPU usage percentage
  - Node-wide memory usage percentage
  - Per-role model readiness (true if model loaded in service)
  - Per-role process health (true if service is running)
  
  These metrics feed observability, dashboards, and auto-scaling decisions.

- **Current State**: `cmd/node-agent/main.go:297-329` returns hardcoded zeros for all metrics:
  ```go
  ProcessUp:  true,        // TODO: actually check process status
  ModelReady: true,        // TODO: actually check model readiness
  VRAMUsedMB: 0,           // TODO: read from nvidia-smi or similar
  QueueDepth: 0,           // TODO: get from role process
  CPUPct:     0,           // TODO: read from /proc/stat
  MemPct:     0,           // TODO: read from /proc/meminfo
  ```

- **Blocked Goal**: The `/api/v1/metrics` endpoint is used by:
  - Load balancer's latency picker (`internal/lb/latency_ewma.go`) to observe per-backend performance
  - Operator dashboards (via `cmd/console`) to visualize cluster health
  - Auto-scaling policies (future work) to detect overload
  - Troubleshooting: operators see all zeros and cannot diagnose performance issues
  
  With all metrics hardcoded, operators cannot monitor health, alerts always fire false-positives, and load balancing is blind to actual backend capacity.

- **Implementation Path**:
  1. **Process status** (`ProcessUp`): Add logic to check if Ollama or SwarmUI process is running
     - For Ollama (chat): `pidof ollama || lsof -i :11434`
     - For SwarmUI (image-gen): `pidof python3 || lsof -i :7860`
     - Cache result with 30-second TTL to avoid hammering
  
  2. **Model readiness** (`ModelReady`): Query service health endpoint
     - Ollama: `GET http://127.0.0.1:11434/api/tags` → check if models list is non-empty
     - SwarmUI: `GET http://127.0.0.1:7860/api/app/models` → similar check
     - Set false if endpoint unreachable or returns empty
  
  3. **VRAM usage** (`VRAMUsedMB`):
     - Primary: Run `nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits` (requires nvidia-utils)
     - Fallback: Read from systemd cgroup `/sys/fs/cgroup/memory/`
     - Return 0 if GPU not present (non-error)
  
  4. **Queue depth** (`QueueDepth`):
     - Ollama: `GET http://127.0.0.1:11434/api/show` → parse response for pending requests
     - SwarmUI: Query internal job queue (requires API extension)
     - Return 0 if no API available
  
  5. **CPU/memory** (`CPUPct`, `MemPct`):
     - Read from `/proc/stat` and `/proc/meminfo` (all platforms except macOS)
     - Parse CPU usage: calculate delta since last read
     - Parse memory usage: `MemAvailable / MemTotal * 100`
     - macOS fallback: `ps aux` or skip with 0
     - Cache with 5-second TTL

- **Testing Strategy**:
  - Unit test: mock process status checks, verify caching
  - Integration test: run actual Ollama/SwarmUI locally, verify metrics are non-zero
  - Regression test: verify metrics respond to service start/stop

- **Dependencies**: None. All collection methods use stdlib (os, io/ioutil) or shell execution.

- **Effort**: Small (2-4 hours). Clear scope, no new dependencies, minimal integration needed.

- **Priority**: MEDIUM — Needed for operational observability but not blocking core functionality.

---

## `k8s-trainer -namespaces` Flag Has No Effect (LOW)

- **Intended Behavior**: `cmd/k8s-trainer` accepts a `-namespaces` flag to specify a custom path to `namespaces.yaml`, allowing users to test training with different namespace definitions without modifying the default config.

- **Current State**: `cmd/k8s-trainer/main.go:17,104,135` parses the flag but the Kubernetes Job always mounts a fixed ConfigMap:
  ```go
  flag.StringVar(&namespacesCfg, "namespaces", "configs/namespaces.yaml", "path to namespaces.yaml")
  // ... later ...
  - "/config/namespaces.yaml"  // HARDCODED, never uses namespacesCfg
  ```
  The flag has no effect; any value passed is ignored.

- **Blocked Goal**: None. This is a utility feature for advanced testing workflows. Core training pipeline does not depend on this flag.

- **Implementation Path** (choose one):
  
  **Option A (Recommended)**: Use the flag value
  1. Pass `namespacesCfg` as a parameter to the job creation function
  2. Mount the specified file path (dynamically) in the Job spec
  3. Update the Job's volumes to reference the correct ConfigMap or file
  
  **Option B (Simpler)**: Remove the flag
  1. Delete the flag definition and all references
  2. Always use the hardcoded `configs/namespaces.yaml` (or make it an env var)
  3. Update documentation to clarify this is not configurable
  
  **Option C (Future)**: Make it environment-variable configurable
  1. Read `K8S_TRAINER_NAMESPACES` env var
  2. Default to built-in config
  3. Remove the command-line flag (confusing)

- **Testing**: None needed; this is low-impact configuration.

- **Effort**: Trivial (30 minutes). Either implement Option B (delete) or Option A (wire through).

- **Priority**: LOW — Affects only advanced usage scenarios. No user-facing impact.

---

## MinIO Object Storage and Backup Unimplemented (DEFERRED)

- **Stated Goal**: `cmd/rag-ingest` documentation and `--backup` flag suggest that embedded chunks and snapshots are exported to MinIO object storage for durability and off-node backup.

- **Current State**: `cmd/rag-ingest/main.go` has:
  - No S3/MinIO client import
  - No object storage configuration flags (bucket, endpoint, creds)
  - `backup()` function (`cmd/rag-ingest/main.go:359-369`) only calls Qdrant's `CreateSnapshot` API; does not upload to any external storage
  - Help text claims backup exists but implementation is Qdrant-only

- **Impact**: Operators believe durable, off-node backups exist when they do not. A node loss means total RAG data loss (only Qdrant snapshot in `/var/qdrant/` remains).

- **Closure Path**:
  - **Option A (Implement)**: Add MinIO client, upload snapshots to `s3://rag/snapshots/` + original files to `s3://rag/<collection>/<hash>/`
  - **Option B (Document)**: Update help text and README to clarify that backups are Qdrant-local only; recommend external Qdrant backup solutions
  
  **Recommendation**: Option B is appropriate for current phase. This is a Phase 2 feature. Document in ROADMAP.md or PLAN.md that backup is limited to Qdrant snapshots.

- **Effort**: If implemented: LARGE (8+ hours, add AWS SDK, credentials, error handling). If documented: SMALL (1 hour).

- **Priority**: LOW — Current RAG service is experimental. Users can manually backup `/var/qdrant/` or use external Qdrant replication.

---

## Additional Observations

### No New Critical Gaps Discovered

The comprehensive audit of the codebase as of 2026-06-05 found that:
- All core execution paths are implemented
- 8 of 13 items from the 2026-06-03 GAPS.md have been fixed
- Remaining gaps are either LOW priority (config flag behavior) or MEDIUM priority (observability metrics)
- No CRITICAL items prevent the stated project goals from being achieved

### Code Quality Improvements (Optional)

The following are recommendations for improving maintainability, not blocking gaps:

1. **Add unit tests for `internal/lb` package** — Pickers have model filtering logic but no isolated tests.
2. **Add integration tests for `internal/discovery` beacon/reconciler** — Auto-discovery is core to the product but lacks end-to-end tests.
3. **Extract main() complexity** — CLI commands like `cmd/gateway/main.go` (188 lines, complexity 33.7) benefit from helper functions to improve readability.
4. **Document metric collection** — Once health metrics are implemented, add a design doc explaining metric update frequency and data sources.

---

**Report Date**: 2026-06-05  
**Previous Report**: 2026-06-03  
**Items Fixed Since Last Report**: 8 of 13  
**Current Open Items**: 3 (1 MEDIUM, 2 LOW)  
**Audit Scope**: Full codebase, all packages, all 80 Go files  
**Methodology**: Static analysis, grep-based gap detection, manual code review against stated goals
