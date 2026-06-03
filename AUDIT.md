# UNIVERSAL BUG AUDIT (END-TO-END) — 2026-06-02

## Project Profile

- **Purpose**: `github.com/opd-ai/cluster` is a Go-first toolkit for operating a
  self-hosted AI cluster: node bootstrap/probe/label, an OpenAI-compatible
  inference **gateway**, a **RAG** service, training/pipeline orchestration
  (`pipeline`, `k8s-trainer`, `fedcoord`, `dataset-build`, `modelfile-gen`),
  model **placement** (`placer`), and a web **console** (server + Ebiten/WASM
  client).
- **Target users**: operators of small/medium self-hosted GPU clusters.
- **Deployment model**: command-line tools run by a trusted operator plus
  long-running network services (`gateway`, `rag`, `console`) that authenticate
  callers with bearer API keys / session tokens. The **inventory file**
  (`cluster/inventory.yaml`) is operator-authored and treated as trusted input.
- **Critical paths** (primary stated goals):
  1. Gateway request routing + auth (`cmd/gateway`).
  2. Model placement decisions (`cmd/placer`).
  3. RAG retrieval/answer (`cmd/rag`).
  4. Cluster bring-up / inventory consistency (`cmd/cluster-*`).
  5. Training-data generation (`cmd/dataset-build`) and federation (`cmd/fedcoord`).
- **Trust boundaries**: untrusted input enters at the gateway/rag/console HTTP
  surfaces (request bodies, query params, bearer tokens). Inventory YAML, flags,
  and key files are operator-controlled (trusted).

## Audit Scope

- **Packages audited**: all 31 `go list ./...` packages — every file under
  `cmd/*` (27 commands) and `internal/{sshutil,tracing,ui,uiapi}`.
- **Functions inspected**: 272 functions + 123 methods (395 callables) across 54
  non-test Go files; all 18 functions with cyclomatic complexity > 10 and all 22
  functions > 50 lines were read manually.
- **go-stats-generator summary**: 6,380 LoC, avg function length 18.2 lines, avg
  cyclomatic complexity 5.5, duplication ratio 2.10%, doc coverage 53.9% overall
  (methods only 13.9%). No circular dependencies.
- **Dynamic evidence**: `go vet ./...` → **0 warnings**; `go test -race ./...` →
  **no test files in any package** (0 tests), so race/panic dynamic evidence is
  **absent** — findings below are from static analysis and data-flow tracing.

## Coverage Log

Legend: ✅ inspected, no finding above LOW · ⚠️ finding recorded · n/a category not applicable.

| Package (cmd/…) | 3b Logic | 3c Nil | 3d Errors | 3e Resources | 3f Concurrency | 3g Security | 3h Aliasing | 3i Init | 3j API |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gateway | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ |
| rag | ⚠️ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ |
| rag-ingest | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| rag-reindex | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| rag-eval | ⚠️ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| placer | ⚠️ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| fedcoord | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| cluster-bootstrap | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| cluster-join | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cluster-probe | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cluster-label | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| pipeline | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| k8s-trainer | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| dataset-build | ✅ | ✅ | ⚠️ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ |
| modelfile-gen | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| eval-harness | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| drain | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| status | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| doctor | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| registry | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| repo-sync | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cache-gc | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| powermetrics-exporter | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| console | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| console-wasm | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cluster-label/repo-sync/etc. utilities | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/sshutil | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | n/a | ✅ | ✅ |
| internal/tracing | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | n/a | ✅ | ✅ |
| internal/ui | ✅ | ✅ | ✅ | ✅ | ✅ | n/a | ✅ | ✅ | ✅ |
| internal/uiapi | ✅ | ✅ | n/a | n/a | n/a | n/a | ✅ | ✅ | ✅ |

All packages were inspected for every applicable category. The audit completed
in a single pass; no packages remain unaudited (see *Remaining Scope*).

## Goal-Achievement Summary

| Stated Goal | Status | Blocking Findings |
| --- | --- | --- |
| Gateway: OpenAI-compatible routing with round-robin + sticky sessions | ✅ | round-robin and sticky cap implemented (`pickBackend`) |
| Gateway: per-key API auth | ✅ | functional (L7 timing note only) |
| Placer: VRAM-aware model placement & multi-device split | ❌ | **C1** — VRAM never parsed; always CPU fallback |
| RAG: hybrid BM25 + dense retrieval | ⚠️ | M5 — IDF is degenerate (per-doc, not corpus) |
| RAG: per-key collection ACL | ⚠️ | M4 — permissive default for keys absent from ACL |
| Cluster bring-up reports failure | ⚠️ | H3 — worker-join failures do not fail the run |
| dataset-build: produce training datasets | ⚠️ | H2 — flush/close errors silently dropped (truncation) |
| Console: live jobs & logs endpoints | ⚠️ | H4 — `/api/jobs` and `/api/logs` always return empty |
| fedcoord: parallel federated rounds | ✅ | now uses goroutines + `sync.WaitGroup` |
| Console: secure session tokens with expiry | ✅ | random tokens + 24h TTL + pruning |

## Findings

### CRITICAL

- [x] **C1 — Placer never reads VRAM; VRAM-aware placement is dead** — `cmd/placer/main.go:282` (`parseInventory`), consumed at `cmd/placer/main.go:134,147` (`buildPlan`) — **bug class: API/behavioral contract + logic** — The hand-rolled inventory parser matches the key `case "vram":`, but the canonical inventory schema written by `cmd/cluster-probe` (`Node.VramGB yaml:"vram_gb"`) and `cluster/inventory.yaml` use **`vram_gb`**. `parseKV` returns key `"vram_gb"`, which never matches `"vram"`, so every node keeps `VRAM == 0` (and `FreeVRAM == 0`). In `buildPlan` the GPU set `n.Healthy && n.VRAM > 0` is therefore always empty, so placer **always** returns the path `"no GPU nodes available; using CPU fallback"`. The documented primary feature (VRAM-ranked placement and `--multi-device` split, `cmd/placer/main.go:1-13`) is fully non-functional on any real inventory. **Data flow:** `cluster-probe` writes `vram_gb: 24` → `placer parseInventory` reads line → `parseKV` → switch has no `vram_gb` case → `VRAM=0` → `buildPlan` GPU list empty → CPU fallback. **Remediation:** change the case label to `"vram_gb"` (or accept both) in `parseInventory`, and convert via `strconv.Atoi(kv[1])` as today. Add a guard logging a warning if a non-empty inventory yields zero VRAM-bearing nodes. Validate with a unit test that parses `cluster/inventory.yaml` and asserts `nodes[0].VRAM == 24`; run `go test -race ./cmd/placer/...` and `go vet ./...`.

### HIGH

- [x] **H2 — dataset-build silently drops flush/close errors → truncated training datasets** — `cmd/dataset-build/main.go:148-149,153-154` (`main`) — **bug class: error handling / resource lifecycle** — `_ = repoWriter.Flush()`, `_ = repoFile.Close()`, `_ = nsWriter.Flush()`, `_ = nsOutFile.Close()` discard errors. On a short write / `ENOSPC` / closed FD, buffered JSONL lines are lost but the tool logs `dataset written` and exits 0. Downstream training then runs on a **silently truncated** dataset. This is a primary-path data-integrity bug. **Remediation:** capture and propagate the `Flush`/`Close` errors (`if err := repoWriter.Flush(); err != nil { log.Fatalf(...) }`; check `Close` likewise). Validate by filling a small tmpfs and asserting the command exits non-zero; `go vet ./...`.

- [x] **H3 — cluster bring-up returns success even when every worker fails to join** — `cmd/cluster-bootstrap/main.go:153-160` and `:171` (`bringUpCluster`) — **bug class: error handling / API contract** — Worker-join errors are logged (`log.Printf("Failed to join worker …")`) and the loop continues; `bringUpCluster` then returns `nil` at line 171 regardless of how many workers failed. An operator running `--up` gets exit code 0 and `✓ k3s control-plane is up` while the cluster has zero (or partial) workers. (Control-plane install errors *do* propagate — only worker joins are demoted.) **Remediation:** accumulate worker-join failures (e.g. `errors.Join`) and return a non-nil error, or print an explicit summary and exit non-zero when any required worker failed. Validate with a dry-run/integration test and `go vet ./...`.

- [x] **H4 — Console `/api/jobs` and `/api/logs` are documented but always return empty** — `cmd/console/main.go:235-250` (`handleJobs`, `handleLogs`); fields declared at `:61-62` — **bug class: API/behavioral contract** — `s.jobs` and `s.logBuf` are read by the handlers but are **never written anywhere** in the package (`pollGateway` only sets `s.state`). Both documented endpoints (`cmd/console/main.go:10-11`) therefore always serialize `null`/`[]`. Operators relying on the console for recent jobs or logs see nothing, with no error signal. **Remediation:** populate `s.jobs`/`s.logBuf` from the gateway poll (or remove the endpoints from the documented API until implemented). Validate by adding a handler test asserting populated output after a poll; `go vet ./...`.

### MEDIUM

- [x] **M1 — modelfile-gen points per-repo `FROM` at a directory, not a GGUF** — `cmd/modelfile-gen/main.go:151,157` (`main`) — **bug class: logic / API contract** — The namespace Modelfile uses `nsGGUF = …/namespace/model.gguf` (a file), but each per-repo Modelfile uses `nsBase = …/namespace/merged` (a **directory**) as the first arg to `ggufPathOrOllamaTag`. When the `merged` directory exists, `os.Stat` succeeds and the rendered `FROM` is a directory path, which Ollama cannot load; when it does not exist, it silently falls back to the base tag. The result is an invalid or inconsistent `FROM` for repo-level models. **Remediation:** reference the namespace GGUF file (`nsGGUF`) or `filepath.Join(nsBase, "model.gguf")` consistently with the namespace case. Validate by generating against a fixture tree and asserting `FROM` ends in `.gguf` or is a tag; `go vet ./...`.

- [x] **M2 — Unbounded video-job map grows for the gateway process lifetime** — `cmd/gateway/videos.go:62-70,149,187` (`videoJobStore.add`) — **bug class: memory / resource** — `globalVideoJobs.jobs` is only ever inserted into; there is no eviction, TTL, or cap (contrast the sticky-session cap at `cmd/gateway/main.go:82`). Every `/v1/videos/generations|edits` request adds a permanent entry. On a long-lived gateway this is a slow memory leak driven by authenticated external input. **Remediation:** prune completed/failed jobs after a retention window (e.g. sweep in `runVideoJob` or a background ticker), or cap the map size with LRU eviction. Validate with a unit test that submits N jobs and asserts the store size stays bounded; `go test -race ./cmd/gateway/...`.

- [x] **M3 — rag-eval recall metric inflated by bidirectional suffix match** — `cmd/rag-eval/main.go:257-265` (`recallHit`) — **bug class: logic** — `strings.HasSuffix(r, e) || strings.HasSuffix(e, r)` treats a retrieved file as a hit whenever either path is a suffix of the other. e.g. expected `"a.md"` matches retrieved `"data.md"` (`"data.md"` ends with `"a.md"`), producing **false-positive** recall hits and overstating retrieval quality — the metric the tool exists to measure. Also `return len(expected) == 0` counts an empty expected set as a hit. **Remediation:** compare on path-segment boundaries (e.g. `r == e` or `strings.HasSuffix(r, "/"+e)`), and return `false`/skip when `expected` is empty. Validate with table tests of (`retrieved`,`expected`) pairs; `go test -race ./cmd/rag-eval/...`.

- [x] **M4 — RAG collection ACL is permissive by default for keys not listed** — `cmd/rag/main.go:424-431` (`collectionAllowed`) — **bug class: security / access control** — When an ACL is configured (`len(collectionACL) > 0`), a request whose bearer key is **absent** from the ACL returns `!ok` → `true`, granting access to *any* collection. Any otherwise-valid API key that the operator simply forgot to add to the ACL bypasses the restriction. This may be intentional opt-in design, but it is undocumented and fails open. *(Uncertainty noted: depends on intended semantics.)* **Remediation:** if the ACL is meant to be authoritative, deny keys absent from it (`return ok && (allowed == "" || allowed == collection)`), and document the behavior. Validate with handler tests for listed/unlisted keys; `go vet ./...`.

- [x] **M5 — RAG "hybrid BM25" IDF is degenerate (per-document, not corpus)** — `cmd/rag/main.go:71-85` (`bm25Score`) — **bug class: logic / API contract** — `df` is derived from whether a term appears in the *single candidate document* (0 or 1), with a hardcoded `N = 1000`. For every matched term `df == 1`, so IDF is a constant (~6.5) and does not down-weight common terms across the corpus, which is the entire point of IDF. The README/GoDoc advertises "hybrid BM25 + dense vector"; the implementation is TF-with-length-normalization times a constant. The code does hedge with "BM25-like … simplified" (`:56-58`), so impact is ranking quality, not correctness. **Remediation:** maintain a corpus document-frequency map (e.g. from a Qdrant metadata collection) and pass real `df` per term; or document that scoring is TF-only. Validate with ranking tests comparing rare vs common query terms.

- [ ] **M6 — fedcoord/k8s-trainer/drain/status pass inventory & flag values into `ssh`/`rsync`/`kubectl` argv without value validation** — `cmd/fedcoord/main.go:134,139,156-172` (`broadcastAdapter`,`triggerLocalTraining`); `cmd/drain/main.go:212`; `cmd/k8s-trainer/main.go:273,282`; `cmd/status/main.go:154` — **bug class: security (argument injection)** — Values such as `n.SSHUser`, `n.Address`, `n.Name`, and namespace/hostname flags flow directly into `exec.Command` arg slices. Shell-metacharacter injection is **not** possible (no shell is invoked), but a value beginning with `-` can be reinterpreted as an option by `ssh`/`rsync`/`kubectl` (e.g. an address like `-oProxyCommand=…`). The inventory is operator-authored (trusted), so practical exploitability is low, hence MEDIUM as defense-in-depth. **Remediation:** validate node/flag fields against an allowlist regex (`^[A-Za-z0-9._@:-]+$` with a leading-`-` reject) before use — `cmd/cluster-bootstrap` already does this for `serverAddr`/`token` (`joinK3sWorker` :236-239); apply the same helper here. Validate with unit tests on the validator; `go vet ./...`.

### LOW

- [ ] **L1 — Dead duplicate RAG-injection code path with orphaned doc comment** — `cmd/gateway/rag.go:216-294` (`injectRAGContext`) and detached comment `:212-215` — **bug class: maintainability/documentation** — `handleChatCompletions` only calls `injectRAGContextRaw`; the `map[string]any` variant `injectRAGContext` is never referenced (confirmed by grep). It duplicates the extraction logic and can silently drift; its GoDoc header (lines 212-215) is detached and reads as a stray comment. **Remediation:** delete the unused function and its orphan comment, or wire one implementation. Validate with `go vet ./...` and `staticcheck` if available.

- [ ] **L2 — `gateway_requests_total` counts unauthenticated requests despite "authenticated" label** — `cmd/gateway/main.go:276` vs help text `:372` (`authMiddleware`,`handleMetrics`) — **bug class: logic/observability** — `gw.reqTotal.Add(1)` runs before the API-key check, so 401-rejected requests are counted, contradicting the metric HELP text "Total authenticated API requests". **Remediation:** increment after a successful key check, or relabel the metric. Validate with a metrics handler test.

- [ ] **L3 — rag-eval ignores `json.Marshal` errors on request bodies** — `cmd/rag-eval/main.go:90,117,144` (`query`,`answer`,`judgeAnswer`) — **bug class: error handling** — `data, _ := json.Marshal(body)`; marshaling these `map[string]any` of strings/ints cannot realistically fail, so impact is theoretical. **Remediation:** check and wrap the error for consistency. Validate with `go vet ./...`.

- [ ] **L4 — cluster-probe truncates VRAM/RAM with integer division** — `cmd/cluster-probe/main.go:338,355` (`detectNvidiaGPU`,`detectAMDGPU`) — **bug class: logic** — `mb / 1024` truncates (e.g. 1536 MB → 1 GB). Most accelerators report power-of-two GB, so impact is minor under-reporting for odd sizes. **Remediation:** round, `(mb + 512) / 1024`. Validate with a unit test on the parse helper.

- [ ] **L5 — placer/console state errors swallowed** — `cmd/placer/main.go:94` (`_ = saveState`), `:314` (`_ = json.Unmarshal` in `loadState`) — **bug class: error handling** — A failed state write or a corrupt state file is treated as empty with no warning, degrading LRU placement history silently. **Remediation:** log the errors. Validate with `go vet ./...`.

- [ ] **L6 — k8s-trainer swallows the final log-stream error** — `cmd/k8s-trainer/main.go:299` (`streamLogs`) — **bug class: error handling** — `_ = kubectlRun(env, "logs", …)`; if log streaming fails the operator gets no signal. Best-effort, hence LOW. **Remediation:** log on error.

- [ ] **L7 — Bearer-key validation is a non-constant-time map lookup** — `cmd/gateway/main.go:286` (`authMiddleware`); `cmd/rag/main.go:417` (`checkAuth`); `cmd/console/main.go:163` (`handleLogin`) — **bug class: security (timing)** — Key comparison via Go map lookup is not constant-time; a remote timing side-channel on API keys is theoretical and hard to exploit over a network. **Remediation:** acceptable as-is for the threat model; if hardened, compare with `crypto/subtle.ConstantTimeCompare` against a hashed key set.

- [ ] **L8 — Per-key daily quota counter increments even when the limit is exceeded** — `cmd/gateway/quotas.go:108-109` (`quotaState.increment`) — **bug class: logic** — The counter is bumped before the `<= limit` check, so a key hammering a maxed endpoint grows its counter without bound until the daily reset. The map is reset each UTC day so growth is bounded per day; impact is negligible. **Remediation:** only increment when under limit, or cap the stored value.

- [ ] **L9 — RAG `topK` is unbounded and over-fetches `topK*3` from Qdrant** — `cmd/rag/main.go:269` (`retrieve`) — **bug class: performance/resource** — `req.TopK` from the (authenticated) request body has no upper bound; `uint64(topK * 3)` can request an enormous Qdrant limit and, for extreme `topK`, the `int` multiply can overflow. Requires a valid API key, so low risk. **Remediation:** clamp `topK` to a sane maximum (e.g. 100) after the `<= 0` default. Validate with a handler test.

- [ ] **L10 — cluster-join does not capture remote stderr** — `cmd/cluster-join/main.go` (`remoteCmd`, sets only `sess.Stdout`) — **bug class: error handling/observability** — Remote command failures lose their stderr diagnostics, making bootstrap failures hard to debug. **Remediation:** also assign `sess.Stderr` (to the same or a separate buffer) and include it in the returned error.

## Metrics Snapshot

| Metric | Value |
| --- | --- |
| Total functions | 272 (+123 methods) |
| Functions above complexity 15 | 0 (max overall-complexity function: `runNamespace`, cyclomatic 18) |
| Functions cyclomatic > 10 | 18 |
| Avg cyclomatic complexity | 5.5 |
| Longest function | `main` (134 lines) |
| Doc coverage (overall) | 53.9% (methods 13.9%) |
| Duplication ratio | 2.10% (21 clone pairs, largest 20 lines) |
| Test pass rate | 0 / 0 (no test files in any package) |
| go vet warnings | 0 |

## False Positives Considered and Rejected

| Candidate | Reason Rejected |
| --- | --- |
| `cluster-label` `VramGB string` vs probe's `int` causes unmarshal failure | Verified empirically: `gopkg.in/yaml.v3` coerces scalar `24` into the `string` field as `"24"` with no error; `buildLabels` works. |
| `cluster-label` nil-map panic on `node.Labels["workload"]` (`:107,127`) | Reading from a nil map in Go returns the zero value; only *writes* panic. Not reachable. |
| `rag-ingest` index-out-of-range on `chunks[i]` vs `vectors[i]` (`:281`) | `embed` enforces `len(result.Data) == len(texts)` (`cmd/rag-ingest/main.go:164-165`); `vectors` length equals `len(chunks)`. Guarded upstream. |
| `cache-gc` divide-by-zero in `diskUsagePct` | Guarded by `if stat.Blocks == 0 { return 0, nil }` (`cmd/cache-gc/main.go:158`). |
| `cluster-bootstrap` SSH client leak in `bringUpCluster` worker loop | `wc.Close()` is called unconditionally each iteration (`:158`); using `defer` in the loop would be the bug, not the fix. No leak path. |
| `fedcoord` shell command injection via `rsync`/`ssh` | No shell is invoked (`exec.Command` argv); classic metacharacter injection impossible. Residual arg-injection risk captured as M6 (LOW-trust, MEDIUM). |
| `rag-ingest` git clone command injection via `--repo` | `exec.CommandContext("git","clone",…,repoURL,dir)` passes argv, not a shell string; not injectable. |
| `placer.probeNode` data race on node fields | Each goroutine writes only its own `*Node`; `gw.backends`/`nodes` slices are not mutated after startup. No shared write. |
| `nightly` reindex fires 60×/night (prior report) | `cmd/rag-reindex` now guards with a `lastNightlyDay` day-key check; the per-minute ticker only triggers ingest once per day. Resolved. |
| `executeBootstrapSteps` always returns nil (prior report) | Now accumulates `errs` and returns `errors.Join(errs...)` (`cmd/cluster-bootstrap/main.go:504-523`). Resolved. |
| Gateway round-robin always picks `candidates[0]` (prior report) | Now uses an atomic `rrIdx` counter (`cmd/gateway/main.go:75,536`). Resolved. |
| Console session token == raw API key, never expires (prior report) | Now issues a 32-byte `crypto/rand` token with a 24h TTL and lazy/active pruning (`cmd/console/main.go:40,173-223`). Resolved. |
| `internal/sshutil` `InsecureIgnoreHostKey` | Gated behind an explicit `-insecure-skip-hostkey-check` flag (default false) with a printed warning; acknowledged pattern. |

## Remaining Scope (if session ended before completion)

| Package | Status | Notes |
| --- | --- | --- |
| (all) | Complete | Full single-pass coverage achieved; no packages deferred. A repeat pass over the LOW set produced no new findings above LOW. |
