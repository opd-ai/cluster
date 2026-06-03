# UNIVERSAL BUG AUDIT (END-TO-END) — 2026-06-03

## Project Profile
`github.com/opd-ai/cluster` is a Go-first self-hosted AI cluster scaffold for cluster lifecycle operations, model training, serving, a web console, and RAG workflows. Target users are operators/developers running local or self-hosted AI infrastructure. Critical paths are the Makefile entry points for bootstrap/up/sync/train/serve/console/rag/status, the gateway, RAG ingestion/query, dataset/training pipeline, cluster SSH/kubectl operations, and the console/WASM UI.

Trust boundaries include operator CLI flags and YAML config files, SSH/kubectl command execution, model/backend HTTP APIs, browser-originated console/gateway requests, remote RAG URLs and Git repos, Qdrant/gRPC, and filesystem paths supplied via flags.

## Audit Scope
All Go packages from `go list ./...` were audited: 31 packages, 54 Go files, 275 functions and 123 methods. Baseline commands were run per task instructions. GitHub research found no open issues or PRs; code/secret scanning alert APIs returned 403. Dependency research found no directly applicable CVEs for the pinned primary dependencies; `chi` v5.3.0 is not affected by GO-2025-3770.

## Coverage Log
| Package | 3b Logic | 3c Nil | 3d Errors | 3e Resources | 3f Concurrency | 3g Security | 3h Aliasing | 3i Init | 3j API |
|---------|----------|--------|-----------|--------------|----------------|-------------|-------------|---------|--------|
| github.com/opd-ai/cluster/cmd/bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/cache-gc | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/cluster-bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/cluster-join | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/cluster-label | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/cluster-probe | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/console | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/console-wasm | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/dataset-build | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/doctor | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/drain | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/eval-harness | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/fedcoord | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/gateway | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/k8s-trainer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/modelfile-gen | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/pipeline | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/placeholder | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/placer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/powermetrics-exporter | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/rag | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/rag-eval | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/rag-ingest | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/rag-reindex | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/registry | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/repo-sync | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/cmd/status | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/internal/sshutil | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/internal/tracing | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/internal/ui | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| github.com/opd-ai/cluster/internal/uiapi | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

## Goal-Achievement Summary
| Stated Goal | Status | Blocking Findings |
|-------------|--------|-------------------|
| Go-first project layout | ✅ | None |
| Centralized Makefile task runner | ⚠️ | Gaps: `make down` placeholder; `make train` calls unsupported repo-sync flag |
| Multi-language linting workflow | ⚠️ | Baseline `go test -race`/`go vet` fail in this environment due missing X11 headers for Ebiten packages |
| WASM console build hook | ⚠️ | Findings H-02/H-03 show async scene state hazards |
| Python training dependency set | ⚠️ | Gaps: dataset-build does not create holdout files claimed by eval-harness |
| Cluster lifecycle bootstrap/up/status/join/drain targets | ⚠️ | M-05/M-07 affect diagnostic and placement correctness |
| RAG workflows | ❌ | H-01 breaks URL ingestion; M-03 affects nightly scheduling |
| OpenAI-compatible gateway | ⚠️ | M-04 may route requests to unintended model backends |

## Findings

### CRITICAL
No CRITICAL findings were confirmed after data-flow and impact review.

### HIGH
- [ ] **URL ingestion always reads the wrong temp path** — `/tmp/workspace/opd-ai/cluster/cmd/rag-ingest/main.go:338-352` — resource/API logic — `fetchURL` writes the downloaded body to `tmpPath` but calls `ing.ingestFile(ctx, tmpPath+".txt")`, so the file opened by `ingestFile` does not exist. Code path: `rag-ingest --url ...` → `main` → `ing.fetchURL` → `os.CreateTemp`/`tmp.Write` → `ingestFile(tmpPath+".txt")` → `os.ReadFile` failure. Concrete consequence: every HTTP/HTTPS URL source is logged as an ingestion error and never reaches Qdrant, making a documented source type non-functional. **Remediation:** In `fetchURL`, create the temp file with a `.txt` suffix and pass the actual created path to `ingestFile`, or add extension handling to `ingestFile` without changing the path. Validate with `go test -race ./cmd/rag-ingest` and a manual `rag-ingest --url http://...` run that logs successful chunk ingestion.
- [ ] **Async console-WASM scenes mutate UI state from goroutines without synchronization** — `/tmp/workspace/opd-ai/cluster/cmd/console-wasm/scene_chat.go:40-53`, `/tmp/workspace/opd-ai/cluster/cmd/console-wasm/scene_imagestudio.go:37-50`, `/tmp/workspace/opd-ai/cluster/cmd/console-wasm/scene_registry.go:47-69` — concurrency/data aliasing — button handlers start goroutines that write `messages`, `busy`, `lastURL`, `progress.Value`, `models`, and `statusMsg` while `Update`/`Draw` read the same fields. Code path: browser click → scene handler → goroutine HTTP call → field writes racing with Ebitengine frame loop. Concrete consequence: native builds can race, and WASM builds can observe partially updated UI state or re-enter fetches because `busy` is unsynchronized. **Remediation:** Add per-scene mutexes or marshal all async results back onto the frame loop before mutating scene fields. Validate with a targeted race-enabled native test/build where supported plus `GOOS=js GOARCH=wasm go build ./cmd/console-wasm`.

### MEDIUM
- [ ] **Gateway model routing uses unsafe prefix matching** — `/tmp/workspace/opd-ai/cluster/cmd/gateway/main.go:555-562` — API/logic — `containsModel` returns true for `strings.HasPrefix(m, model)`, so a request for `llama` can match backends that only advertise `llama2`, `llama3`, or unrelated prefixed aliases. Code path: `/v1/chat/completions`/`/v1/completions`/`/v1/embeddings` → `pickBackend` → `containsModel`. Concrete consequence: requests can be routed to a backend that lacks the exact requested model or to an unintended model family. **Remediation:** Require exact model ID matches or restrict prefix aliases to documented separators such as `model+":"` only. Validate with `go test -race ./cmd/gateway` including model-name collision cases.
- [ ] **Nightly RAG reindex day key is calendar-inaccurate** — `/tmp/workspace/opd-ai/cluster/cmd/rag-reindex/main.go:130-138` — logic/scheduling — the dedupe key uses `utc.YearDay() + utc.Year()*366`, which is not a real monotonic day number for non-leap years. Code path: long-running `rag-reindex` ticker → nightly hour comparison → `lastNightlyDay` dedupe. Concrete consequence: year-boundary arithmetic is fragile and can skip or duplicate a nightly full ingest in long-lived processes. **Remediation:** Replace the expression with `int(utc.Unix()/86400)` or a formatted UTC date string. Validate with a unit test around Dec 31/Jan 1 and `go test -race ./cmd/rag-reindex`.
- [ ] **Dataset JSON marshal/write errors are ignored** — `/tmp/workspace/opd-ai/cluster/cmd/dataset-build/main.go:203-218` — error handling/data integrity — `filepath.Rel`, `json.Marshal`, and writer errors are discarded while building JSONL. Code path: `make train` → `pipeline` → `dataset-build` → `walkRepo`. Concrete consequence: a bad relative path, marshal failure, or failed write can silently produce incomplete/corrupt training datasets while the command reports success. **Remediation:** Check `filepath.Rel`, `json.Marshal`, `repoW.Write`, `repoW.WriteByte`, `nsW.Write`, and `nsW.WriteByte`, returning an error from the walk callback on failure. Validate with `go test -race ./cmd/dataset-build` and a full `make train` dry run where applicable.
- [ ] **Eval harness sends completion requests without request context and ignores marshal errors** — `/tmp/workspace/opd-ai/cluster/cmd/eval-harness/main.go:204-224` — error handling/resource lifecycle — `generate` uses `body, _ := json.Marshal(...)` and `http.NewRequest` rather than `NewRequestWithContext`, so callers cannot cancel a stuck completion beyond client timeout and marshal failures become empty request bodies. Code path: `eval-harness` → `evaluate` → `generate`. Concrete consequence: failed serialization or cancelled evaluations are not correctly propagated, causing misleading eval failures and slower shutdowns. **Remediation:** Pass a `context.Context` into `generate`, check marshal errors, and create the request with `http.NewRequestWithContext`. Validate with `go test -race ./cmd/eval-harness` and a cancellation test.
- [ ] **Placer treats malformed Ollama tag responses as healthy empty-model nodes** — `/tmp/workspace/opd-ai/cluster/cmd/placer/main.go:222-240` — error handling/API — `probeNode` sets `node.Healthy = resp.StatusCode == http.StatusOK` before decoding `/api/tags`; on decode error it returns without marking unhealthy. Code path: `placer` → `probeNodes` → `probeNode` → `buildPlan`. Concrete consequence: a node returning malformed JSON remains healthy with empty model data and may receive incorrect placement decisions. **Remediation:** On decode failure, log the error and set `node.Healthy=false` before returning. Validate with `go test -race ./cmd/placer` using a malformed `/api/tags` test server.
- [ ] **Doctor reports unparsable disk capacity as 0 GB failure** — `/tmp/workspace/opd-ai/cluster/cmd/doctor/main.go:469-485` — error handling/diagnostics — `strconv.ParseInt` error is ignored, so unexpected `df` output becomes `kb=0` and emits a false FAIL rather than a parse warning. Code path: `doctor` remote checks → `checkRemoteDiskSpace`. Concrete consequence: operators can chase false disk-space failures and miss the real command/output compatibility issue. **Remediation:** Check the parse error and return WARN with the raw output/error when parsing fails. Validate with `go test -race ./cmd/doctor` and a malformed remote output test.

### LOW
- [ ] **Doctor silently masks unparsable MTU output** — `/tmp/workspace/opd-ai/cluster/cmd/doctor/main.go:512-530` — error handling/diagnostics — `strconv.Atoi` errors are ignored, causing the generic “Unable to verify MTU” response without preserving the parse failure. **Remediation:** Check the parse error and include it in the WARN message. Validate with `go test -race ./cmd/doctor`.
- [ ] **Console session and static asset auth are not documented in README feature claims** — `/tmp/workspace/opd-ai/cluster/cmd/console/main.go:90-144` — documentation/API — the console allows public loading of root/WASM bootstrap assets but protects other assets/API routes with bearer session tokens. This is reasonable behavior but not captured in the README’s console description, making deployment expectations unclear. **Remediation:** Document the console auth/static asset model in README or console docs. Validate with markdown lint.

## Metrics Snapshot
| Metric | Value |
|--------|-------|
| Total functions | 275 |
| Functions above complexity 15 | 2 |
| Functions longer than 50 lines | 22 |
| Avg cyclomatic complexity | 3.80 |
| Doc coverage | 53.91% overall |
| Duplication ratio | 2.26% |
| Test pass rate | 29/31 packages listed before environment build failure |
| go vet warnings | 1 build-blocking missing `X11/Xlib.h` dependency chain |

## Baseline Results
- `go-stats-generator analyze` completed after using `$(go env GOPATH)/bin/go-stats-generator`; the binary was not on PATH immediately after install.
- `go test -race ./...` failed because `github.com/hajimehoshi/ebiten/v2/internal/glfw` requires `X11/Xlib.h` in this Linux environment. Packages not depending on the Ebiten UI reported `[no test files]` before the build failure.
- `go vet ./...` failed for the same missing X11 header.

## False Positives Considered and Rejected
| Candidate | Reason Rejected |
|-----------|----------------|
| SSH shell command construction in `cluster-bootstrap`, `cluster-join`, and `fedcoord` | Values embedded into shell commands are constrained by `shellSafe`/argument validation; dangerous characters and leading `-` are rejected where relevant. |
| `internal/ui.Button.Update` nil callback panic | Current code already checks `b.OnClick != nil` before invocation. |
| `drawNodeCard` nil sparkline panic | Current code checks `if sl != nil` before calling `SetBounds`/`Draw`. |
| `cmd/console/main.go` failed websocket upgrade leaks client channel | `handleWS` uses a defer immediately after registration to delete the channel from `s.clients` on all returns. |
| `cmd/console/ws.go` websocket read goroutine leak | The accepted connection is closed with `defer conn.CloseNow()` and the read loop shares a cancellable request context; no confirmed unbounded leak path was found. |
| `cmd/rag` Qdrant payload type assertions | Helper functions use checked type assertions and return zero values for absent/mismatched payloads; this can reduce result quality but does not panic. |
| `cmd/gateway` JSON error body formatting | `%q` is used inside JSON object strings, so backend error strings are escaped. |
| `cmd/rag-ingest` `git clone` command injection | `exec.CommandContext` passes the repo URL as an argument without shell interpretation. |
| `cmd/repo-sync` git argument injection | Git is invoked without a shell; repository URL remains an argv item. |
| `cmd/cache-gc` skipping unreadable walk entries | Cache garbage collection can reasonably skip inaccessible files and continue; no critical correctness claim requires failing the whole run. |

## Remaining Scope (if session ended before completion)
| Package | Status | Notes |
|---------|--------|-------|
| All packages | Completed | No remaining audit scope. |
