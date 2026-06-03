# UNIVERSAL BUG AUDIT (END-TO-END) — 2026-06-03

## Project Profile
- **Purpose**: `github.com/opd-ai/cluster` is a Go-first toolkit for operating a self-hosted AI
  cluster: k3s bring-up, node auto-discovery and deployment, an OpenAI-compatible inference
  gateway (chat/image/video), a RAG service, a fine-tuning pipeline, and an Ebitengine/WASM web
  console. Python (`python/`) is reserved for training code.
- **Target users**: cluster operators and contributors running the binaries in `cmd/*` via the
  top-level `Makefile`.
- **Deployment model**: long-running network services (gateway, node-agent, console, rag) plus
  one-shot CLIs (cluster-bootstrap, cluster-join, drain, pipeline, dataset-build, etc.). Trust
  boundaries: HTTP/WebSocket clients (gateway, console, rag, node-agent), UDP discovery beacons,
  YAML inventory/config files, and SSH/kubectl shell-outs.
- **Critical paths** (implement the primary stated goals):
  1. Fine-tuning pipeline: `cmd/pipeline` → `cmd/repo-sync` → `cmd/dataset-build` → training →
     `cmd/modelfile-gen`/registry.
  2. Inference serving: `cmd/gateway` + `internal/lb` + `internal/pipeline`.
  3. RAG: `cmd/rag`, `cmd/rag-ingest`.
  4. Node lifecycle: `cmd/node-agent`, `internal/discovery`, `internal/autotuner`, `cmd/node-deploy`.

## Audit Scope
- **Packages audited**: all 40 packages returned by `go list ./...` (29 `cmd/*`, 11 `internal/*`).
- **Functions inspected**: 480 (313 functions + 167 methods) across 80 non-test files.
- **go-stats-generator metrics**: 7,861 LoC, avg function length 18.7 lines, avg cyclomatic
  complexity 5.5, 26 functions with cyclomatic >10 (5 with >15), doc coverage 70.5% overall
  (methods 42.4%), duplication ratio 1.73%.
- **Baseline tooling**: `go vet ./...` clean (0 warnings); `go test -race ./...` passes but is
  **vacuous — there are zero test files in the repository**, so race/panic evidence is weak.
  High-complexity hotspots manually inspected: `internal/pipeline.executeStage` (21),
  `cmd/node-agent.main` (18), `cmd/pipeline.runNamespace` (18), `internal/discovery.run` (18),
  `internal/autotuner.BudgetSplit` (15), `cmd/gateway.main` (14).

## Coverage Log
All checklist categories (3b Logic, 3c Nil/Boundary, 3d Errors, 3e Resources, 3f Concurrency,
3g Security, 3h Aliasing, 3i Init, 3j API) were completed for every package below.

| Package | 3b | 3c | 3d | 3e | 3f | 3g | 3h | 3i | 3j |
|---------|----|----|----|----|----|----|----|----|----|
| internal/autotuner | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/discovery | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/inventory | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/lb | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/nodeapi | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/pipeline | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/serviceinstall | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/sshutil | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/tracing | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/ui | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/uiapi | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-ingest | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-reindex | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-eval | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/dataset-build | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/eval-harness | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/k8s-trainer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/modelfile-gen | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/pipeline | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/console | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/console-wasm | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/node-agent | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/node-deploy | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/fedcoord | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/registry | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/placer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-join | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-label | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-probe | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/doctor | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/drain | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cache-gc | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/status | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/repo-sync | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/powermetrics-exporter | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/placeholder | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

## Goal-Achievement Summary
| Stated Goal | Status | Blocking Findings |
|-------------|--------|-------------------|
| Run the full fine-tuning pipeline (`make train`) | ❌ | C1, C2, C4 |
| Persist/back up RAG data to MinIO (`cmd/rag-ingest`) | ❌ | H9 |
| Ingest documents into Qdrant (`cmd/rag-ingest`) | ❌ | H8 |
| RAG query/answer on authenticated deployments (`cmd/rag`, gateway) | ❌ | H3, M4 |
| Inference gateway routes requests by role **and model** (`internal/lb`) | ⚠️ | H5 |
| Pipeline status API reports real state (`cmd/gateway`) | ❌ | C3 |
| Video edits use the supplied video (`cmd/gateway`) | ❌ | H1 |
| Node auto-discovery exposes peers (`cmd/node-agent`) | ❌ | M1 |
| `make down` gracefully stops services | ❌ | (see GAPS.md) |
| `make status` diffs declared vs actual state | ⚠️ | H10 |
| `make sync` synchronizes repos reliably | ⚠️ | H11 |
| Multi-role resource budgeting (`internal/autotuner`) | ⚠️ | H14 |

## Findings

### CRITICAL
- [x] **Fine-tuning datasets contain Git metadata, not source code** — `cmd/dataset-build/main.go:175-206` (`walkRepo`) with `cmd/repo-sync/main.go:116-123` (`cloneRepo`) — Logic/data corruption — `repo-sync` creates **bare** clones (`git clone --bare --filter=blob:none`), which have no working tree; `dataset-build` then `filepath.WalkDir`s the bare repo and only skips `objects`/`refs`/`logs`/dot-dirs (line 186). The remaining readable files in a bare repo are `config`, `HEAD`, `description`, `packed-refs`, `info/*`, and `hooks/*.sample` — so the generated `dataset.jsonl` is populated with Git plumbing files and sample shell hooks instead of the repositories' actual source. Every fine-tuning dataset on the primary `make train` path is corrupt. **Remediation:** read blobs from the bare repo via `git --git-dir <repo> ls-tree -r --name-only HEAD` + `git --git-dir <repo> show <oid>` (or sync non-bare worktrees), keying on tree entries rather than on-disk files. Validate with `go run ./cmd/dataset-build -namespace <ns> -repo-cache repo-cache -out datasets && head -1 datasets/<ns>/dataset.jsonl` and confirm real source content.
- [x] **`make train` sync stage aborts immediately: unknown `-namespace` flag** — `cmd/pipeline/main.go:143` vs `cmd/repo-sync/main.go:64-69` — API/behavioral contract — `runNamespace` invokes `repo-sync` with `-namespace ns.Name`, but `repo-sync` only defines `-namespaces`, `-cache-dir`, `-depth`, `-jobs`, `-dry-run`. Go's `flag` package exits non-zero with `flag provided but not defined: -namespace`, so the documented full pipeline fails before any dataset/training work. **Remediation:** add a `-namespace` filter flag to `repo-sync` (mirroring `dataset-build`), or drop the unsupported argument at `cmd/pipeline/main.go:143`. Validate with `go run ./cmd/repo-sync -namespace x -dry-run` returning success.
- [x] **Gateway `GET /v1/pipelines/{id}` always reports `completed`** — `cmd/gateway/pipelines.go:69-78` (`handleGetPipelineStatus`) — API/behavioral contract — the handler ignores the real execution and returns a hardcoded `{"status":"completed"}` placeholder for any ID. Clients polling pipeline status see running, failed, or nonexistent pipelines as successfully completed, leading them to consume absent outputs. **Remediation:** persist `PipelineExecution` results by ID (e.g. a `sync.Map` keyed on `spec.ID`) in `handleSubmitPipeline`, look them up here, and return `404` for unknown IDs. Validate with `go test ./cmd/gateway` plus a submit→status round-trip.
- [x] **Evaluation workflow is non-functional: holdout sets are never produced** — `cmd/dataset-build/main.go:118` (only writes `dataset.jsonl`) and `cmd/eval-harness/main.go:251` (missing holdout → `nil, nil`) — API/behavioral contract — `eval-harness` documents reading `holdout.jsonl` produced by `dataset-build`, but `dataset-build` exposes no `-holdout-ratio` and writes only `dataset.jsonl`; `eval-harness` then treats the missing holdout as zero examples and reports success. Regression evaluation silently "passes" without evaluating anything. **Remediation:** add holdout splitting (`-holdout-ratio`) to `dataset-build` writing `holdout.jsonl` per namespace/repo, and make `eval-harness` return an error when the holdout is absent (unless `--allow-empty`). Validate with `go run ./cmd/dataset-build ... && test -s datasets/<ns>/holdout.jsonl`.

### HIGH
- [x] **Load balancer ignores the requested `model`** — `internal/lb/picker.go:52-75`, `internal/lb/least_queue.go:22`, `internal/lb/latency_ewma.go:29` — Logic/API contract — all three `Pick(role, model, hint)` implementations filter only by `Healthy && hasRole`, never consulting `BackendRecord.Models`, despite `BackendRegistry.Pick`'s doc claiming selection "for the given role **and model**". A request/stage for a specific model can be routed to a backend that does not serve it, causing downstream 404/inference failures. **Remediation:** add a shared `supportsModel(b, model)` guard (empty `model` = any) to all three pickers' candidate filters. Validate with `go test ./internal/lb` (add table tests covering mismatched models).
- [x] **RAG ingestion writes invalid Qdrant UUID point IDs** — `cmd/rag-ingest/main.go:267` — Logic/data — point IDs are built as `fmt.Sprintf("%s-%d", hash[:16], i)` (16 hex chars + `-N`) and passed as a `PointId_Uuid`, which is not a valid RFC-4122 UUID. Qdrant rejects non-UUID/non-uint64 IDs, so the primary ingestion upsert fails. **Remediation:** derive a valid UUIDv5 from `hash`+chunk index (e.g. `uuid.NewSHA1`) or use a numeric `PointId_Num`. Validate by ingesting against a live Qdrant and confirming the upsert succeeds (`go run ./cmd/rag-ingest -dir testdata`).
- [x] **RAG retrieval does not forward `Authorization`, breaking authenticated deployments** — `cmd/rag/main.go:391` — Auth/error handling — the embedding call in the retrieve path omits the caller's bearer token, so when the gateway has `GATEWAY_API_KEYS` set, `/v1/embeddings` returns 401 and every `/rag/query` and `/rag/answer` fails. **Remediation:** thread the inbound `Authorization` header through `retrieve` → `embed` and set it on the embeddings request. Validate with `go test ./cmd/rag` using an auth-required gateway stub.
- [x] **`response_format=b64_json` returns a path/URL, not base64** — `cmd/gateway/images.go:181-195` (`buildImageResponse`) — API contract — for `b64_json`, the handler places the SwarmUI image path/URL straight into the `b64_json` field instead of fetching the bytes and base64-encoding them. OpenAI-compatible clients fail to decode the image. **Remediation:** when `format == "b64_json"`, GET the image from SwarmUI and emit `base64.StdEncoding`-encoded bytes. Validate with `go test ./cmd/gateway`.
- [x] **`POST /v1/videos/edits` silently ignores the supplied video** — `cmd/gateway/videos.go:194-198` (`handleVideoEdits`) — Logic — the handler copies only `Prompt`, `Model`, and `Image` from `videoEditRequest` into the generation request, dropping `req.Video`. Video-to-video edits run as if no source video was provided, producing wrong output with no error. **Remediation:** forward `req.Video` into the backend request (or return `400` if video edits are unsupported). Validate with `go test ./cmd/gateway`.
- [ ] **node-agent pipeline/info/metrics APIs are unauthenticated** — `cmd/node-agent/main.go:130-139` — Security — the agent registers `/api/v1/*` routes (including `pipeline/submit` and `pipeline/result/...`) on a bare `chi`/`http.ServeMux` with no auth middleware. Any host on the LAN/tailnet can submit jobs to a node or read others' results. **Remediation:** add a shared-secret/bearer middleware (or mTLS) in front of the agent routes, consistent with the gateway. Validate that an unauthenticated request returns 401 (`go test ./cmd/node-agent`).
- [ ] **Pipeline executor polls a failed backend forever** — `internal/pipeline/executor.go:191-200` — Resource/error handling — in the result-poll loop, any non-200 response (`continue` at line 199) and network error (`continue` at line 194) are retried indefinitely; for a stage with no `Timeout` (`stageCtx` is only `WithCancel(ctx)`), a backend permanently returning 404/500 keeps the request goroutine alive until the caller cancels. **Remediation:** distinguish permanent 4xx/5xx from transient errors, and add a bounded max-poll duration or retry count. Validate with `go test ./internal/pipeline` against a stub that returns persistent 500s.
- [x] **`make sync` reports success while repositories fail to sync** — `cmd/repo-sync/main.go:92-104` — Error handling — worker goroutine errors are logged but never aggregated or returned; `main` exits 0 regardless. CI and operators believe the repo cache is fresh when clones/fetches failed. **Remediation:** collect per-repo errors (channel + `errors.Join`) and exit non-zero if any failed. Validate with `go test ./cmd/repo-sync` using an unreachable repo URL.
- [x] **`make status` reports "No drift" (exit 0) when the cluster is unreachable** — `cmd/status/main.go:106` — Error handling/API contract — a failed `kubectl get nodes` is logged and ignored, so the diff proceeds with empty actual state and the tool exits 0 even though it documents exit code 2 on drift/error. Operators get a false "in sync" signal. **Remediation:** return a fatal error / exit 2 when node retrieval fails. Validate with `go test ./cmd/status` using a failing kubectl stub.
- [x] **Pipeline conversion failures are logged but not propagated** — `cmd/pipeline/main.go:277` — Error handling — GGUF/modelfile/registry conversion errors are logged and execution continues, so `cmd/pipeline` can exit 0 with missing artifacts on the primary `make train` path. **Remediation:** aggregate conversion errors and return them so the namespace/run fails. Validate by forcing a converter failure and asserting a non-zero exit.
- [x] **Each video request spawns an unbounded goroutine + permanent map entry** — `cmd/gateway/videos.go:164,200` — Resource lifecycle/DoS — every `/v1/videos/generations`/`edits` call does `go gw.runVideoJob(job)` and `globalVideoJobs.add(job)` with no concurrency cap; pruning only removes terminal jobs after a retention window. A client (open mode, or any valid key) can exhaust memory, GPU, and backend sockets. **Remediation:** enforce a max in-flight job count per key/global and reject excess with `429`. Validate with `go test ./cmd/gateway`.
- [x] **`--url`/`--repo` lists are mangled by colon-splitting** — `cmd/rag-ingest/main.go:446` (`splitColon`) — Logic/boundary — multi-value flags are split on `:`, which corrupts any `https://...` URL into `https`, `//host/path`, etc. URL and many repo-URL ingestion sources never resolve. **Remediation:** accept repeatable flags or split on comma/newline instead of `:`. Validate with `go test ./cmd/rag-ingest`.
- [x] **MinIO storage/backup is documented but never implemented** — `cmd/rag-ingest/main.go:13,18,359-369` — API/behavioral contract — the package doc claims step 4 "Write raw file + chunks to MinIO" and that `--backup` "exports to MinIO rag/snapshots/", but no MinIO/S3 client exists; `backup()` only calls Qdrant `CreateSnapshot` and logs it. Operators believe durable off-node backups exist when none are written. **Remediation:** implement the MinIO upload of raw files/chunks and snapshot export, or correct the documentation and `--backup` help to state Qdrant-snapshot-only behavior. Validate by running `--backup` and confirming the MinIO object exists.
- [x] **VRAM budgeting over-allocates and starves the highest-need role on scarce GPUs** — `internal/autotuner/colocation.go:69-94` (`BudgetSplit`) — Arithmetic/logic — when scaled allocations round to 0 they are bumped to 1 (line 74), but the running total is not capped against `hw.VramGB`; e.g. with 2 GB VRAM and roles `chat,image-generation,training`, the first two each get 1 GB (total 2 GB) and `training` (the last, highest-requirement role) gets `2 - 2 = 0`. The node advertises 2 GB allocated on 2 GB hardware while training has no VRAM budget. **Remediation:** track remaining VRAM and clamp each allocation to it; give the remainder to the highest-priority role, not unconditionally the last. Validate with `go test ./internal/autotuner` (add scarce-VRAM cases).

### MEDIUM
- [x] **node-agent `/api/v1/peers` always returns an empty list** — `cmd/node-agent/main.go:256-263` — API/behavioral contract — `h.peers` is initialized empty (line 125) and never written; received discovery beacons are not recorded, so the discovery API always reports zero peers. **Remediation:** populate `h.peers` from the discovery listener under `peersMu`. Validate with `go test ./cmd/node-agent`.
- [x] **node-agent HTTP server has no timeouts** — `cmd/node-agent/main.go:139` — Resource lifecycle/DoS — the server sets no `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`; slow-loris clients can pin connections and exhaust goroutines. **Remediation:** configure `http.Server` timeouts. Validate with `go test ./cmd/node-agent`.
- [ ] **node-agent pipeline jobs are retained forever** — `cmd/node-agent/main.go:294` — Resource lifecycle — submitted jobs accumulate in the in-memory store with no TTL/eviction, growing memory unboundedly over the process lifetime. **Remediation:** add TTL/max-size eviction for terminal jobs. Validate with `go test ./cmd/node-agent`.
- [x] **Gateway RAG context injection ignores RAG HTTP status** — `cmd/gateway/rag.go:105` — Error handling — a 401/403/500 from the RAG service is decoded as empty context and the `rag` tool is silently dropped, so prompts run without retrieval and without surfacing the failure. **Remediation:** check `resp.StatusCode` before decoding and propagate/log the error. Validate with `go test ./cmd/gateway`.
- [x] **`COLLECTION_ACL` revokes all keys lacking an explicit entry** — `cmd/rag/main.go:443` — Logic/API contract — once any ACL entry exists, keys without an entry are denied, contradicting the documented default-allow behavior; adding one restricted collection locks out every other key. **Remediation:** after `checkAuth`, return allow when a key has no ACL entry (default-allow). Validate with `go test ./cmd/rag`.
- [x] **Image `n` and `size` are not upper-bounded** — `cmd/gateway/images.go:202` (`normalizeImageParams`) — Input validation/DoS — clients can request very large `n`/dimensions, overloading SwarmUI/GPU. **Remediation:** cap `n` and allowlist sizes. Validate with `go test ./cmd/gateway`.
- [x] **Video `width`/`height` are not upper-bounded** — `cmd/gateway/videos.go:284-291` — Input validation/DoS — only non-positive values are defaulted; arbitrarily large dimensions reach the backend. **Remediation:** clamp/allowlist dimensions. Validate with `go test ./cmd/gateway`.
- [x] **Image backend request not bound to client context** — `cmd/gateway/images.go:150` (`callSwarm`) — Resource lifecycle — the SwarmUI call does not use `r.Context()`, so a client disconnect leaves backend work running until the gateway's own timeout. **Remediation:** plumb `r.Context()` into `callSwarm`/`http.NewRequestWithContext`. Validate with `go test ./cmd/gateway`.
- [ ] **eval-harness ignores non-2xx generation responses** — `cmd/eval-harness/main.go:226` — Error handling — a 500 from the gateway becomes an empty generation that is scored as a (bad) model result rather than an infra error, skewing metrics. **Remediation:** check status before decoding; treat non-2xx as error. Validate with `go test ./cmd/eval-harness`.
- [ ] **rag-eval ignores non-2xx HTTP responses** — `cmd/rag-eval/main.go:172` — Error handling — same class as above for the RAG-eval HTTP helper. **Remediation:** check status and include the body in the error. Validate with a 500 test server.
- [ ] **rag-eval recall is binary, not the documented fraction** — `cmd/rag-eval/main.go:266` — Logic/metric — recall counts "any expected file present" instead of the fraction of expected files retrieved, inflating recall when only one of several expected files appears. **Remediation:** compute the per-item fraction over all expected files. Validate with multi-file QA cases.
- [ ] **rag-reindex path matching lacks a separator boundary** — `cmd/rag-reindex/main.go:184` — Boundary/logic — raw `strings.HasPrefix` means a change under `repo-cache/foo2` triggers reindex for `repo-cache/foo`. **Remediation:** compare cleaned paths with a trailing separator or `filepath.Rel`. Validate with adjacent repo names.
- [ ] **rag-reindex never watches newly created directories** — `cmd/rag-reindex/main.go:191` — Resource/lifecycle — fsnotify is not recursive; files created under new subdirectories are missed until the nightly reindex. **Remediation:** on create-dir events, add a watch recursively. Validate by creating a new subdir + file under a watched root.
- [ ] **k8s-trainer writes a manifest to a predictable temp path** — `cmd/k8s-trainer/main.go:189` — Security/temp file — the manifest path is predictable in a world-writable temp dir, enabling a local symlink/pre-create attack before `os.Create`. **Remediation:** use `os.CreateTemp("", jobName+"-*.yaml")` with mode 0600. Validate with a symlink-precreation test.
- [ ] **k8s-trainer `-namespaces` flag is ignored** — `cmd/k8s-trainer/main.go:135` — API/behavioral contract — the flag is parsed but the Job always mounts `/config/namespaces.yaml` from the ConfigMap, so a custom file has no effect. **Remediation:** mount the requested file or remove the flag. Validate by inspecting the rendered manifest.
- [ ] **k8s-trainer leaks failed Jobs despite `-cleanup=true`** — `cmd/k8s-trainer/main.go:218` — Resource lifecycle — `log.Fatalf` on job failure/timeout exits before the cleanup block, leaving the Kubernetes Job behind. **Remediation:** `defer` cleanup immediately after a successful apply. Validate with a forced failing Job.
- [ ] **`pipeline -dry-run` still executes the llama.cpp setup script** — `cmd/pipeline/main.go:257` — API/behavioral contract — `convert` runs `tools/setup-llama-cpp.sh` directly (not via the dry-run-aware `runCmd`), so dry-run can download/build/mutate state. **Remediation:** route the script through `runCmd` or skip it in dry-run. Validate with `go run ./cmd/pipeline -only convert -dry-run`.
- [ ] **dataset-build does not sanitize repo labels, diverging from repo-sync's cache path** — `cmd/dataset-build/main.go:126` — Path handling — `repo-sync` stores caches under `sanitizeLabel(label)` (replacing `/`, space, `:` with `_`), but `dataset-build` reads `filepath.Join(repoCache, repo.Label)` raw, so labels containing those characters look in the wrong directory and are skipped. **Remediation:** share one `sanitizeLabel` helper between the two commands. Validate with a label containing `/`.
- [ ] **rag-ingest URL fetch does not check HTTP status** — `cmd/rag-ingest/main.go:321-335` — Error handling — `fetchURL` reads the body regardless of status, so 404/500 error pages are embedded as source documents. **Remediation:** reject non-2xx before reading. Validate by ingesting a local 404 endpoint.
- [ ] **Discovery listener dedup map grows unbounded from unauthenticated beacons** — `internal/discovery/listener.go:150` — Security/resource — any multicast sender can emit unique `address` values, growing the `seen` map without bound in long-running listeners/node-agents. **Remediation:** expire `seen` entries by time/size and validate the address format. Validate with `go test ./internal/discovery`.
- [ ] **cache-gc watermarks are not range-validated** — `cmd/cache-gc/main.go:59` — Boundary/logic — only `high > low` is checked, not `0 <= low < high <= 100`; a negative low-water evicts every eligible cache file. **Remediation:** enforce `0 <= low < high <= 100`. Validate with `go test ./cmd/cache-gc`.
- [ ] **doctor remote report can never pass** — `cmd/doctor/main.go:127` — Logic/init — `performRemoteChecks` starts with `AllPassed=false` and never sets it true, so a remote doctor exits 1 even when all checks pass. **Remediation:** initialize `report.AllPassed = true` and clear it on the first failure. Validate with `go test ./cmd/doctor`.
- [ ] **fedcoord `-dry-run` still creates the aggregate output directory** — `cmd/fedcoord/main.go:235` — API/behavioral contract — `MkdirAll` runs before the dry-run guard, so dry-run mutates the filesystem. **Remediation:** check the dry-run flag before `MkdirAll`. Validate with `go test ./cmd/fedcoord`.
- [ ] **placer accepts `max-devices <= 0` and emits a zero-node plan** — `cmd/placer/main.go:178` — Logic — an invalid flag silently yields an unusable multi-device placement with no nodes. **Remediation:** reject `max-devices < 1`. Validate with `go test ./cmd/placer`.
- [ ] **placer `containsModel` matches on prefix** — `cmd/placer/main.go:198` — Logic — a request for `llama` matches a node loaded with `llama2`, selecting the wrong node. **Remediation:** match exact model name or `model + ":"`. Validate with `go test ./cmd/placer`.
- [ ] **console-wasm RAG query builds a URL without escaping** — `cmd/console-wasm/scene_rag.go:74` — Security/logic — the query text is concatenated into the URL unescaped, allowing parameter injection (e.g. overriding `top_k`). **Remediation:** build the query with `url.Values.Encode()`. Validate with `GOOS=js GOARCH=wasm go test ./cmd/console-wasm`.

### LOW
- [ ] **repo-sync repo URL from config can be parsed as a git option** — `cmd/repo-sync/main.go:121-123` — Security (config-trust) — a YAML URL beginning with `-` is passed positionally to `git clone`, potentially altering behavior. Inventory/config is trusted, so impact is low. **Remediation:** reject URLs starting with `-` and insert `--` before the URL. Validate with `go test ./cmd/repo-sync`.
- [ ] **cluster-join can write a join script outside the target dir via inventory hostname** — `cmd/cluster-join/main.go:185` — Path traversal (config-trust) — an inventory hostname containing `../` escapes `--script DIR`; reachable only with a malicious inventory file. **Remediation:** validate hostnames as RFC-1123 names or use `filepath.Base` plus a containment check. Validate with `go test ./cmd/cluster-join`.
- [ ] **cluster-label/drain hostnames are passed to kubectl without `--`** — `cmd/cluster-label/main.go:89`, `cmd/drain/main.go:147-149` — Option injection (config-trust) — values originate from the inventory allowlist (`drain` requires an exact `findNode` match at `cmd/drain/main.go:101`, `cluster-label` iterates inventory nodes), so `--all`/`-l=` are not reachable from untrusted input; defense-in-depth only. **Remediation:** validate Kubernetes node names and insert `--` before positional node names where supported. Validate with `go test ./cmd/cluster-label ./cmd/drain`.
- [ ] **`NewBeacon(0, ...)` panics in `time.NewTicker` inside the goroutine** — `internal/discovery/beacon.go:71` — Boundary — a non-positive interval is accepted by the constructor but panics at `Start`. **Remediation:** reject `interval <= 0` in `NewBeacon`. Validate with `go test ./internal/discovery`.
- [ ] **`NewListener(-1)` panics at `make(chan ..., -1)`** — `internal/discovery/listener.go:90` — Boundary — a negative buffer size crashes the caller. **Remediation:** normalize `bufferSize <= 0`. Validate with `go test ./internal/discovery`.
- [ ] **`NewSparkline(-1).Push` panics on slice bounds** — `internal/ui/sparkline.go:45` — Boundary — a negative `maxSamples` computes an out-of-range index. **Remediation:** clamp/reject `maxSamples < 1`. Validate with `GOOS=js GOARCH=wasm go test ./internal/ui`.
- [ ] **Gateway opt-in telemetry uses `http.DefaultClient` with no timeout** — `cmd/gateway/main.go:294` — Resource lifecycle — a stalled telemetry endpoint can hang the goroutine indefinitely. **Remediation:** use a client with a finite timeout. Validate with `go test ./cmd/gateway`.
- [ ] **Gateway LoRA reload only adds, never removes, adapters** — `cmd/gateway/lora.go:83` — Config lifecycle — adapters deleted from the manifest remain routable until restart. **Remediation:** rebuild the adapter map atomically from the manifest. Validate with `go test ./cmd/gateway`.
- [ ] **repo-sync `-jobs <= 0` starts no workers and exits successfully** — `cmd/repo-sync/main.go:92` — Boundary — zero/negative parallelism syncs nothing but reports success. **Remediation:** require `cfg.Jobs >= 1`. Validate with `go test ./cmd/repo-sync`.
- [ ] **drain SSH-agent socket is not closed** — `cmd/drain/main.go:250` (`signerFromAgent`) — Resource lifecycle — the agent Unix socket connection is leaked. **Remediation:** `defer conn.Close()` after a successful dial. Validate with `go test ./cmd/drain`.
- [ ] **powermetrics-exporter `-interval <= 0` panics** — `cmd/powermetrics-exporter/main.go:100` — Boundary — a non-positive interval reaches `time.NewTicker`. **Remediation:** reject `interval <= 0` before starting. Validate with `go test ./cmd/powermetrics-exporter`.
- [ ] **rag-eval `*_ms` JSON fields serialize nanoseconds** — `cmd/rag-eval/main.go:51` — API/behavioral contract — `time.Duration` fields named `_ms` marshal as nanoseconds, off by 1e6 for consumers. **Remediation:** serialize explicit millisecond integers. Validate by inspecting output JSON.
- [ ] **Gateway API-key check is not constant-time** — `cmd/gateway/main.go:335` — Security (theoretical) — map lookup of the bearer token is not constant-time; timing side channels on API keys are impractical over HTTP but noted. **Remediation:** acceptable as-is; optionally use `crypto/subtle` over a known key set. No validation required.

## Metrics Snapshot
| Metric | Value |
|--------|-------|
| Total functions (incl. methods) | 480 (313 funcs + 167 methods) |
| Functions above complexity 15 | 5 |
| Functions above complexity 10 | 26 |
| Avg cyclomatic complexity | 5.5 |
| Doc coverage (overall / methods) | 70.5% / 42.4% |
| Duplication ratio | 1.73% |
| Test pass rate | 0/0 (no test files exist) |
| go vet warnings | 0 |

## False Positives Considered and Rejected
| Candidate | Reason Rejected |
|-----------|-----------------|
| `cmd/rag-ingest` URL written to one temp path but a different `.txt` ingested (prior GAPS claim) | Fixed: lines 338-352 create `rag-url-*.txt`, store `tmp.Name()`, write it, and ingest the same `tmpPath`. |
| `cmd/drain` `kubectl cordon`/`drain` argument injection via `--all`/`-l=` (CRITICAL candidate) | Hostname must exactly equal an inventory entry (`findNode`, `cmd/drain/main.go:101`); `--all` never matches. Downgraded to LOW (config-trust only). |
| video job data race between status read and writer | Reads use `getSnapshot` value-copy under `RWMutex`; writers lock `globalVideoJobs.mu` (`cmd/gateway/videos.go:79-102,262-278`). |
| `internal/autotuner.runCmd` shell exec injection | Commands are internal constant probes, annotated `//nolint:gosec`; no user-controlled string. |
| `internal/sshutil` `InsecureIgnoreHostKey` | Gated behind an explicit `insecure-skip` flag that logs a MITM warning; intentional opt-in. |
| console token generation weakness | Uses `crypto/rand` 32-byte tokens (`cmd/console/main.go:205`), not `math/rand`. |
| console static-asset auth bypass | Public allowlist is exact-match; all other paths require session auth. |
| WebSocket `token` query-param auth | Documented design; token is session-scoped, distinct from API keys. |
| `cmd/pipeline` passing `-namespace` to dataset-build/modelfile-gen | Both define `-namespace` (`cmd/dataset-build/main.go:99`, `cmd/modelfile-gen/main.go:112`). |
| Unclosed HTTP response bodies in audited clients | All audited `client.Do`/`Get` paths `defer resp.Body.Close()`. |
| Unsafe type assertions in qdrant/RAG payload helpers | Use the comma-ok two-value form. |
| `internal/pipeline` `time.After` in poll loop "leak" | Timer fires and is consumed every iteration; not an accumulating leak. |
| Loop-variable capture in rag-reindex timer closure | Captures `dirCopy`, not the changing event path. |
| fedcoord SSH command injection | `exec.Command("ssh", a...)` is not shell-expanded and node args are inventory-validated. |
| Go nil-map reads in label/bootstrap | Reads from a nil map are safe in Go. |

## Remaining Scope (session completed)
| Package | Status | Notes |
|---------|--------|-------|
| (all 40 packages) | Audited | A full pass over every package and function was completed; no package was sampled or skipped. |
