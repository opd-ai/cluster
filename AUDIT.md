# UNIVERSAL BUG AUDIT (END-TO-END) — 2026-06-01

## Project Profile

**Module**: `github.com/opd-ai/cluster` (Go 1.25.0)  
**Purpose**: Self-hosted AI cluster scaffold — inference gateway (OpenAI-compatible), RAG pipeline, federated fine-tuning, node bootstrap/join, browser console, model registry, and training orchestration.  
**Target users**: Self-hosters running GPU nodes on bare metal or k3s; small-to-medium AI teams.  
**Deployment model**: Operator-managed; nodes are SSH-reachable; gateway and RAG server are long-running daemons; training is batch-job-oriented.  
**Critical paths**:
1. Gateway → inference backends (chat completions, image, video)
2. RAG ingest → Qdrant → RAG query → gateway injection
3. Cluster bootstrap → k3s node join
4. Placer / FedCoord → model placement and federated training

---

## Audit Scope

| Item | Value |
|------|-------|
| Packages audited | 26 (23 `cmd/`, `internal/sshutil`, `internal/uiapi`, `internal/ui`) |
| Total Go source files | 49 |
| Total functions + methods | 361 |
| Lines of code | 5,678 |
| Test files | **0** |
| go vet warnings | 0 (build failures in console-wasm / internal/ui due to missing X11 headers in CI — not code bugs) |
| go test -race | all [no test files] |

---

## Coverage Log

| Package | 3b Logic | 3c Nil | 3d Errors | 3e Resources | 3f Concurrency | 3g Security | 3h Aliasing | 3i Init | 3j API |
|---------|----------|--------|-----------|--------------|----------------|-------------|-------------|---------|--------|
| cmd/cache-gc | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-bootstrap | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-join | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-label | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/cluster-probe | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/console | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/console-wasm | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/dataset-build | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/doctor | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/eval-harness | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/fedcoord | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/main.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/images.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/lora.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/quotas.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/rag.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/gateway/videos.go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/k8s-trainer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/modelfile-gen | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/pipeline | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/placer | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/placeholder | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-eval | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-ingest | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/rag-reindex | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/registry | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| cmd/repo-sync | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/sshutil | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/uiapi | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| internal/ui | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

---

## Goal-Achievement Summary

| Stated Goal | Status | Blocking Findings |
|-------------|--------|-------------------|
| OpenAI-compatible inference gateway | ⚠️ | M5 (round-robin broken), M4 (sticky growth), H8 (connection exhaustion) |
| Multi-node cluster bootstrap | ⚠️ | H1 (bootstrap never reports failure), M2/M3 (shell injection) |
| Model placement across GPU nodes | ❌ | H2 (inventory key mismatch — placer produces no output) |
| Federated fine-tuning | ❌ | H3 (inventory key mismatch — no nodes found), M15 (sequential, not parallel) |
| RAG-augmented chat | ⚠️ | H4/H5 (no timeout), M12 (no status check), L2 (BM25 inaccuracy) |
| Nightly RAG re-indexing | ⚠️ | H7 (60× over-execution per night) |
| Model registry + download | ⚠️ | H6 (no download timeout) |
| Secure multi-user console | ⚠️ | M8 (token = API key, no expiry), C2 (data race in video jobs) |
| Doctor / self-check | ⚠️ | C1 (crash if user.Current() fails) |

---

## Findings

### CRITICAL

- [ ] **C1 — Nil pointer dereference panic in `doctor`** — `cmd/doctor/main.go` `loadSSHKey` (≈line 130) — Nil safety — `usr, _ := user.Current()` discards the error; if `user.Current()` fails, `usr` is `nil` and the next line `filepath.Join(usr.HomeDir, ...)` panics, crashing the process. This is the only path that provides an SSH key when `-key` is not supplied, so any system where `user.Current()` fails leaves the operator with an unrecoverable crash.  
  **Data flow**: `main()` calls `loadSSHKey("")` → `user.Current()` returns `(nil, err)` → `usr.HomeDir` → panic.  
  **Remediation**: Replace `usr, _ := user.Current()` with:
  ```go
  usr, err := user.Current()
  if err != nil {
      return nil, fmt.Errorf("resolve home directory: %w", err)
  }
  ```
  Validate with `go vet ./cmd/doctor/...` and a manual invocation in a minimal container where `/etc/passwd` is absent.

- [ ] **C2 — Data race: `handleVideoJobStatus` reads `videoJob` fields without lock** — `cmd/gateway/videos.go` — Concurrency — `handleVideoJobStatus` calls `globalVideoJobs.get(id)` which returns a raw `*videoJob` pointer under `s.mu.RLock()`, then releases the lock before calling `writeJSON(w, job)`. Concurrently, `runVideoJob` calls `setJobStatus` and direct field assignments (`job.Status`, `job.VideoURL`, `job.GifURL`, `job.UpdatedAt`) under `globalVideoJobs.mu.Lock()`. The JSON encoder in `writeJSON` reads all fields of the struct without holding any lock, racing with the writer.  
  **Data flow**: HTTP handler goroutine: `get(id)` (releases lock) → `json.Encode(job)` (reads Status, VideoURL, GifURL). Background goroutine: `setJobStatus` (acquires lock, writes same fields) — concurrent, no synchronization.  
  **Remediation**: Return a snapshot value instead of a pointer:
  ```go
  func (s *videoJobStore) getSnapshot(id string) (videoJob, bool) {
      s.mu.RLock()
      defer s.mu.RUnlock()
      j, ok := s.jobs[id]
      if !ok {
          return videoJob{}, false
      }
      return *j, true // copy under lock
  }
  ```
  Also protect the direct field assignments in `runVideoJob` (lines that set `job.Status = jobCompleted`, `job.VideoURL`, etc.) within `setJobStatus` or the existing `globalVideoJobs.mu.Lock()` block.  
  Validate with `go test -race ./cmd/gateway/...` (requires a test file to exist) or run the gateway under `-race` and submit a video job.

---

### HIGH

- [ ] **H1 — `executeBootstrapSteps` always returns nil; bootstrap failures are silently ignored** — `cmd/cluster-bootstrap/main.go` `executeBootstrapSteps` (≈line 260) — Error handling — The function loops over shell steps and prints a warning when a step fails, but always returns `nil`. The caller `bootstrapNode` therefore reports success even when all steps fail. A GPU driver install failure, k3s install failure, or any other critical step is silently ignored.  
  **Data flow**: `bootstrapNode` → `executeBootstrapSteps` → `return nil` unconditionally. `bootstrapNode` returns `nil` to `bootstrapCluster`, which continues to the next node.  
  **Remediation**: Accumulate non-idempotent errors and return them:
  ```go
  var errs []error
  if err != nil && !isIdempotentError(step, output) {
      errs = append(errs, fmt.Errorf("step %q: %w", step, err))
  }
  // ...
  return errors.Join(errs...)
  ```
  Validate by intentionally introducing a failing step and confirming the command exits non-zero.

- [ ] **H2 — `parseInventory` reads `- name:` but inventory format uses `- hostname:`; placer output is always empty** — `cmd/placer/main.go` `parseInventory` (≈line 210) — Logic — The inventory YAML produced by `cluster-bootstrap`, `cluster-join`, and `cluster-probe` uses the key `hostname:` for node names. `parseInventory` in `placer` looks for `strings.HasPrefix(trim, "- name:")`. Because the field name never matches, every parsed node has an empty `Name` field, and `buildPlan` produces placement output that references nodes with no hostname. The placer is functionally broken against any real inventory file.  
  **Data flow**: `main()` calls `parseInventory(inventoryPath)` → no `"- name:"` matches → all nodes have `Name: ""` → `buildPlan` generates placement plan with empty node names.  
  **Remediation**: Change `"- name:"` to `"- hostname:"` in `parseInventory`; ensure the extracted value is assigned to the correct field. Also add a check that discards nodes with empty hostnames.  
  Validate: `placer -inventory <real-inventory.yaml>` should produce non-empty node entries.

- [ ] **H3 — `loadFedNodes` reads `- name:` but inventory format uses `- hostname:`; fedcoord finds no nodes** — `cmd/fedcoord/main.go` `loadFedNodes` (≈line 180) — Logic — Same root cause as H2. `loadFedNodes` parses `- name:` but the inventory uses `- hostname:`. Every parsed node has an empty hostname; SSH connections use `"@"` as the target, which will fail. Federated training coordination is non-functional against real inventory files.  
  **Remediation**: Same fix as H2 — change `"- name:"` to `"- hostname:"` and validate non-empty hostnames before attempting SSH.

- [ ] **H4 — `embed` in `cmd/rag` uses `http.DefaultClient` (no timeout); HTTP handler goroutine blocks indefinitely** — `cmd/rag/main.go` `embed` (≈line 310) — Resource lifecycle — `http.DefaultClient` has no `Timeout`. If the gateway is overloaded or unresponsive, the `embed` call blocks the HTTP handler goroutine servicing `/rag/query` or `/rag/answer` forever. Under sustained load this leaks goroutines until the process runs out of file descriptors or memory.  
  **Data flow**: `/rag/query` handler → `chatWithContext` → `embed` → `http.DefaultClient.Do(req)` → blocks.  
  **Remediation**: Propagate the request context or create a client with a bounded timeout:
  ```go
  req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
  client := &http.Client{Timeout: 60 * time.Second}
  resp, err := client.Do(req)
  ```
  Consider making the client a field on `server` so it is reused across calls.  
  Validate: introduce an artificial 65-second hang in a test gateway and confirm the embedding call returns an error.

- [ ] **H5 — `embed` in `cmd/rag-ingest` uses `http.DefaultClient` (no timeout); ingestion goroutine blocks indefinitely** — `cmd/rag-ingest/main.go` `embed` (≈line 160) — Resource lifecycle — Same pattern as H4. Long-running `rag-ingest` invocations can stall permanently at the embedding step with no timeout or cancellation.  
  **Remediation**: Same as H4 — pass context and use a bounded client or set `http.DefaultClient.Timeout`.

- [ ] **H6 — `downloadFile` uses `http.Get` with no context or timeout; model downloads block forever** — `cmd/registry/main.go` `downloadFile` (≈line 90) — Resource lifecycle — `http.Get(url)` uses `http.DefaultClient` with no `Timeout`. A stalled or slow HTTP server causes `registry pull` to hang indefinitely with no user-visible progress or exit.  
  **Data flow**: `runPull` → `downloadFile` → `http.Get(url)` → blocks.  
  **Remediation**: Replace with a context-aware request using a client that has a streaming read deadline:
  ```go
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  // use a client without a global Timeout (streams can be large) but set
  // ResponseHeaderTimeout on the transport to detect stalled servers early.
  ```
  Validate: point `registry pull` at a non-responsive server and confirm it times out within a reasonable bound.

- [ ] **H7 — Nightly re-ingest ticker fires every minute; full re-ingest runs up to 60× per hour instead of once** — `cmd/rag-reindex/main.go` `main` (≈line 90) — Logic — `nightly` is a `time.NewTicker(1 * time.Minute)`. Each tick checks `t.UTC().Hour() == *nightlyHour`. During the target hour (e.g., 02:00–02:59 UTC) this condition is true for all 60 ticks. Each tick fires a full re-ingest of every RAG-enabled repo, multiplying LLM embedding API calls and Qdrant upserts by 60× compared to a once-per-night design. On a large corpus this exhausts API quotas and degrades Qdrant performance.  
  **Remediation**: Track the last-triggered day number and skip if already triggered today:
  ```go
  var lastNightlyDay int
  // inside the ticker case:
  today := t.UTC().YearDay()
  if t.UTC().Hour() == *nightlyHour && today != lastNightlyDay {
      lastNightlyDay = today
      // run full re-ingest
  }
  ```
  Validate: run `rag-reindex` in a test environment with a 1-minute nightly window and confirm one re-ingest fires, not 60.

- [ ] **H8 — New `http.Client` created per request in `proxyTo`; connection pooling disabled for all inference traffic** — `cmd/gateway/main.go` `proxyTo` (≈line 230) — Performance — `proxyTo` creates `&http.Client{Timeout: 5 * time.Minute}` on every call. A new `http.Client` carries its own `http.Transport`, which has no pooled connections. Under load (e.g., 100 concurrent chat requests), this opens 100 separate TCP connections to each backend per second, exhausts ephemeral port capacity, and adds significant TLS/TCP handshake latency. The same pattern appears in `callSwarm` (images.go) and `retrieveRAGContext` (rag.go).  
  **Remediation**: Create a single shared `http.Client` as a field of `Gateway` (or a package-level var) initialized once:
  ```go
  type Gateway struct {
      // ...
      httpClient *http.Client
  }
  // in newGateway:
  gw.httpClient = &http.Client{Timeout: 5 * time.Minute}
  ```
  Pass per-request context via `http.NewRequestWithContext`. Apply the same fix to `callSwarm`, `retrieveRAGContext`, and `generateVideo`.  
  Validate: benchmark `wrk -t4 -c100 -d30s http://gateway/v1/chat/completions` before and after; confirm connection count decreases.

---

### MEDIUM

- [ ] **M1 — Integer overflow in `diskUsagePct`: `used * 100` overflows `uint64` on large filesystems** — `cmd/cache-gc/main.go` `diskUsagePct` (≈line 120) — Arithmetic — `used := stat.Blocks - stat.Bfree` (both `uint64`); `used * 100 / stat.Blocks` — the multiplication overflows when `used > 1.8×10¹⁷` blocks (approximately 92 PB at 512-byte blocks). On such a filesystem the computed percentage wraps to a small number, suppressing legitimate eviction. While 92 PB is uncommon today, this is a latent correctness bug.  
  **Remediation**: Reorder to avoid overflow: `return int(used / stat.Blocks * 100), nil` — or use integer-safe division: `return int(used * 100 / stat.Blocks)` is safe only if `stat.Blocks` is already divided; the cleanest fix is `return int((float64(used) / float64(stat.Blocks)) * 100), nil`.  
  Validate with `go test -run TestDiskUsagePct ./cmd/cache-gc/...` (new test) with a synthetic `syscall.Statfs_t`.

- [ ] **M2 — Shell injection in `joinScript` via unescaped `serverAddr` and `token`** — `cmd/cluster-join/main.go` `joinScript` (≈line 140) — Security — `serverAddr` (from `-server` flag) and `token` (from SSH command output on the control node) are interpolated directly into a shell script heredoc without sanitization. If a compromised control node returns a token containing shell metacharacters (`;`, `\``, `$(`, etc.), the injected commands execute with `sudo` privileges on the worker node.  
  **Remediation**: Validate that `serverAddr` matches a hostname/IP pattern and that `token` is alphanumeric+hyphen before use, or pass values via environment variables (`K3S_TOKEN` is already accepted as an env var by the k3s installer).  
  Validate: pass `token='; rm -rf /tmp/test'` and confirm the sanitizer rejects or escapes it.

- [ ] **M3 — Shell injection in `joinK3sWorker` via unescaped `token`** — `cmd/cluster-bootstrap/main.go` `joinK3sWorker` (≈line 290) — Security — Same pattern as M2. `token` retrieved from the control node via SSH is embedded verbatim in a `fmt.Sprintf`-constructed shell command. A malicious or compromised control node can inject arbitrary commands executed with `sudo` on worker nodes.  
  **Remediation**: Same as M2 — validate token format or use environment variable passing.

- [ ] **M4 — Sticky session map grows without bound; potential memory exhaustion under sustained load** — `cmd/gateway/main.go` `pickBackend` (≈line 170) — Memory — `gw.sticky[key+":"+model]` is written on every new (api-key, model) combination but is never evicted. In a long-running gateway with many clients or rotating API keys, the map grows indefinitely. At scale this is an unbounded memory leak.  
  **Remediation**: Add an eviction policy — either an LRU map (e.g., `golang.org/x/lru`) or a TTL with periodic cleanup. Alternatively, limit sticky sessions to a configurable maximum entry count with random eviction.  
  Validate: run the gateway under `GODEBUG=gctrace=1` with key rotation and confirm heap does not grow unboundedly.

- [ ] **M5 — "Round-robin" in `pickBackend` always selects the first healthy backend** — `cmd/gateway/main.go` `pickBackend` (≈line 170) — Logic — The comment says "round-robin fallback" but when no sticky session exists the code selects `candidateIdx[0]` — the lowest-index healthy backend — on every call. There is no cycling counter or atomic index. All traffic is consistently routed to a single backend, defeating load distribution.  
  **Data flow**: Any request without an existing sticky session → `pickBackend` → `candidates[0]` always selected.  
  **Remediation**: Add an atomic counter:
  ```go
  var rrIdx atomic.Uint64
  // in pickBackend:
  idx := int(rrIdx.Add(1)) % len(candidates)
  chosen := candidates[idx]
  ```
  Validate: route 100 requests with no sticky sessions and confirm they distribute across backends.

- [ ] **M6 — Manual YAML parsers cannot handle standard YAML; fragile against normal formatting** — `cmd/cluster-bootstrap/main.go`, `cmd/cluster-join/main.go`, `cmd/cluster-probe/main.go`, `cmd/cluster-label/main.go` — All four commands include a hand-rolled `loadInventory` / `parseInventory` function that counts indentation characters and splits on `: `. It cannot handle YAML anchors, quoted string values, inline comments, multi-line values, or values containing colons. A comment added by a user or editor (e.g., `# worker node: europe`) or any non-trivial YAML feature silently corrupts the parsed data. The project already depends on `gopkg.in/yaml.v3`.  
  **Remediation**: Replace all four manual parsers with `yaml.Unmarshal` into a typed struct. Define the inventory schema once in an internal package.  
  Validate: round-trip a YAML file containing quoted values, anchors, and comments through the new parser and assert correct output.

- [ ] **M7 — `mergeHyperparams` cannot override numeric config values to zero** — `cmd/dataset-build/main.go` `mergeHyperparams` (≈line 210) — Logic — The function treats `0` as "not set" for `LearningRate`, `WarmupSteps`, `MaxSteps`, and `Epochs`. A namespace that explicitly wants to train with `max_steps: 0` (early-stopping only) or `warmup_steps: 0` cannot express this; the value falls back to the global default.  
  **Remediation**: Use pointer fields (`*float64`, `*int`) in the hyperparams struct so that `nil` represents "not set" and `0` is a valid explicit value. Alternatively, introduce a sentinel (e.g., `-1`) for "use global default."  
  Validate: set `max_steps: 0` in a namespace config and confirm the generated training command passes `--max-steps 0`.

- [ ] **M8 — Session token equals the API key; no expiry; leaked token grants permanent access** — `cmd/console/main.go` `handleLogin` (≈line 80) — Security — A comment in the code reads "For now, the session token is the API key itself (simple stateless auth). Production should use a signed JWT with short expiry." The token never expires. A leaked browser session (e.g., via XSS in log output rendered in the console, browser history, or a shared screen) provides permanent gateway access because the token validates directly against `GATEWAY_API_KEYS`.  
  **Remediation**: Issue a short-lived signed JWT (e.g., using `golang.org/x/crypto/bcrypt` for key derivation or a standard JWT library) with a configurable expiry (default 8 hours). Store only the JWT in the browser, not the raw API key.  
  Validate: confirm that after expiry the token is rejected at `/api/ws` and `/api/v1/...` endpoints.

- [ ] **M9 — Training deadline loop exits silently when deadline passes with no terminal job state** — `cmd/k8s-trainer/main.go` `main` (≈line 140) — Logic — The deadline polling loop exits with `break` when `time.Now().After(deadline)`, but only calls `log.Fatalf` if the job status is `Failed`. If the deadline passes while the job is still `Pending` or `Running`, the loop breaks silently and `main` returns normally (exit 0). The caller (pipeline or CI) receives a success exit code for a training job that never completed.  
  **Remediation**: After the `break`, check whether the job reached a terminal state:
  ```go
  if status != "Succeeded" && status != "Failed" {
      log.Fatalf("training job %s did not complete within deadline (last status: %s)", jobName, status)
  }
  ```
  Validate: set a 1-second deadline against a job that takes 60 seconds and confirm exit code is non-zero.

- [ ] **M10 — gRPC connection not closed on SIGTERM; resource leak on normal process shutdown** — `cmd/rag/main.go` `main` (≈line 50) and `cmd/rag-ingest/main.go` `main` (≈line 80) — Resource lifecycle — Both programs place `defer conn.Close()` in `main()`, which only executes when `main` returns. `http.ListenAndServe` blocks until error; on SIGTERM the OS kills the process without running deferred cleanup. Qdrant gRPC streams and keep-alive goroutines are not gracefully torn down.  
  **Remediation**: Implement graceful shutdown:
  ```go
  srv := &http.Server{Addr: *listenAddr, Handler: mux}
  go func() {
      c := make(chan os.Signal, 1)
      signal.Notify(c, os.Interrupt, syscall.SIGTERM)
      <-c
      ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
      defer cancel()
      _ = srv.Shutdown(ctx)
  }()
  defer conn.Close()
  ```
  Validate: `kill -TERM <pid>` and confirm `conn.Close()` log line appears.

- [ ] **M11 — `authMiddleware` uses exclusive `Mutex.Lock()` for a read-only map lookup; serializes all authenticated requests** — `cmd/gateway/main.go` `authMiddleware` (≈line 110) — Performance — `gw.apiKeys` is populated once at startup and never written again. `authMiddleware` acquires `gw.mu.Lock()` (exclusive) to read from it on every request. With `sync.Mutex` (not `sync.RWMutex`), this serializes every authenticated HTTP request through a single lock, including long-running streaming completions that hold the lock for the duration of the auth check. Under concurrent load this becomes a throughput bottleneck.  
  **Remediation**: Change `Gateway.mu` to `sync.RWMutex` and use `gw.mu.RLock()` in `authMiddleware`, `handleListModels`, and any other read-only path. Retain `gw.mu.Lock()` for writes in `probeAll` / `reloadAdapters`.  
  Validate: benchmark at 100 concurrent requests and confirm no contention on the auth lock.

- [ ] **M12 — HTTP status code not checked after embedding API call; non-200 responses silently produce wrong data** — `cmd/rag/main.go` `embed` (≈line 315) and `cmd/rag-ingest/main.go` `embed` (≈line 160) — Error handling — After `client.Do(req)`, neither function checks `resp.StatusCode`. If the gateway returns HTTP 401 or 500, the body contains a JSON error object, not an embedding response. `json.Decode` into the embeddings struct either succeeds with empty `data` array or fails with an unhelpful decode error. In both cases the real problem (auth failure, quota exceeded) is hidden.  
  **Remediation**:
  ```go
  if resp.StatusCode != http.StatusOK {
      body, _ := io.ReadAll(resp.Body)
      return nil, fmt.Errorf("embed API returned %d: %s", resp.StatusCode, body)
  }
  ```
  Validate: configure an invalid API key and confirm the error message includes the 401 status.

- [ ] **M13 — API key passed as command-line argument to `rag-ingest`; visible in process listing** — `cmd/rag-reindex/main.go` `runIngest` (≈line 205) — Security — `exec.Command(binary, "--api-key", apiKey, ...)` passes the API key as a CLI argument. On Linux, command-line arguments are readable by any local user via `/proc/<pid>/cmdline`. An unprivileged attacker on the same host can harvest the gateway API key.  
  **Remediation**: Pass the key via an environment variable instead: `cmd.Env = append(os.Environ(), "GATEWAY_API_KEY="+apiKey)` and change `rag-ingest` to read from `os.Getenv("GATEWAY_API_KEY")` when the flag is empty (it already does via `os.Getenv` as the flag default).  
  Validate: launch `rag-reindex`, read `/proc/<pid>/cmdline` for the `rag-ingest` child, and confirm the key is absent.

- [ ] **M14 — VRAM heuristic assumes 8 GiB per model; severely underestimates large models** — `cmd/placer/main.go` `buildPlan` (≈line 130) — Logic — `used := len(node.LoadedModels) * 8` hardcodes 8 GiB per model. A Llama-3-70B or Mixtral-8x7B model requires 40–80 GiB of VRAM. The underestimate causes the placer to route additional models to a node that is already at capacity, resulting in out-of-memory OOM kills on the backend.  
  **Remediation**: Add a model-size field to the probed backend state (query `/api/tags` which includes the model size) or add a configurable `vram_per_model` field to the inventory. Remove the hardcoded constant.  
  Validate: probe a node running a 70B model and confirm the planner correctly identifies it as having < 8 GiB free.

- [ ] **M15 — `triggerLocalTraining` trains nodes sequentially; defeats federated parallelism** — `cmd/fedcoord/main.go` `triggerLocalTraining` (≈line 220) — Performance/Correctness — Nodes are iterated in a `for range` loop with a blocking SSH call per node. For `N` nodes each taking `T` minutes to train, total time is `N×T` minutes. Federated training's primary benefit — parallel local training — is unrealized.  
  **Remediation**: Launch one goroutine per node using a `sync.WaitGroup`:
  ```go
  var wg sync.WaitGroup
  for _, node := range nodes {
      wg.Add(1)
      go func(n FedNode) {
          defer wg.Done()
          // SSH training call
      }(node)
  }
  wg.Wait()
  ```
  Validate: time a run with 2 nodes and confirm total time is approximately `max(T1, T2)` rather than `T1 + T2`.

---

### LOW

- [ ] **L1 — SSH agent connection never closed in `getAgentSigner`/`agentSigner` across four commands** — `cmd/cluster-bootstrap/main.go`, `cmd/cluster-join/main.go`, `cmd/cluster-probe/main.go`, `cmd/doctor/main.go` — Resource lifecycle — `net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))` opens a connection to the SSH agent that is assigned to a local variable and never closed. Each `getAgentSigner` call leaks one file descriptor for the duration of the process.  
  **Remediation**: Return the `net.Conn` alongside the signer and close it with `defer conn.Close()` in the caller, or close it after the SSH client is dialed.

- [ ] **L2 — BM25 implementation omits IDF; common terms not penalized; retrieval accuracy degraded** — `cmd/rag/main.go` `bm25Score` (≈line 260) — Logic — The function computes only the TF part of BM25 (term frequency with length normalization). It omits IDF (inverse document frequency), which is the component that distinguishes rare, informative terms from common stop-words. Without IDF, queries containing "the", "is", "a" produce inflated scores for irrelevant documents.  
  **Remediation**: Build an IDF index at server startup from the Qdrant collection, or compute IDF per query over the retrieved candidates. At minimum document the known omission.  
  Validate: a query for "the model" should not outscore a query for "transformer architecture" on a technical corpus.

- [ ] **L3 — `chunkText` enters an infinite loop when `overlap-tokens >= chunk-tokens`** — `cmd/rag-ingest/main.go` `chunkText` (≈line 110) — Logic — `start += chunkSize - overlapSize`. When `overlapSize >= chunkSize` (user passes `--overlap-tokens >= --chunk-tokens`), this step is ≤ 0 and the loop never advances, hanging the process.  
  **Remediation**: Add input validation:
  ```go
  if overlapToks >= chunkToks {
      return nil, fmt.Errorf("overlap-tokens (%d) must be less than chunk-tokens (%d)", overlapToks, chunkToks)
  }
  ```
  Validate: `rag-ingest -chunk-tokens 10 -overlap-tokens 10 -dir /tmp/test` should return an error, not hang.

- [ ] **L4 — `probeLoop` and `startLoRAWatcher` goroutines have no shutdown mechanism** — `cmd/gateway/main.go` `probeLoop` and `cmd/gateway/lora.go` `startLoRAWatcher` — Resource lifecycle — Both run `for { time.Sleep(...) }` with no context cancellation or stop channel. They leak at process shutdown (the OS reclaims resources) but prevent clean unit test teardown if these are ever invoked in tests.  
  **Remediation**: Accept a `context.Context` parameter and check `ctx.Done()` in the loop condition.

- [ ] **L5 — `http.FileServer` serves entire `wasmDir` without content filtering** — `cmd/console/main.go` `registerRoutes` (≈line 60) — Security — `http.FileServer(http.Dir(s.wasmDir))` serves any file present in the configured directory, including hidden files (`.env`, SSH keys) if the operator misconfigures `wasmDir` to a broad path. No authentication is required to access the file server.  
  **Remediation**: Add a `http.StripPrefix` / allowlist that serves only `.wasm`, `.js`, `.html`, and `.css` extensions, and log a startup warning if `wasmDir` is set to a home directory or filesystem root.

- [ ] **L6 — Manual YAML output in `outputYAML` does not escape special characters** — `cmd/cluster-probe/main.go` `outputYAML` (≈line 175) — Logic — Values are emitted directly into YAML via `fmt.Fprintf(buf, "  address: %s\n", node.Address)`. A hostname or address containing `:` or `#` produces malformed YAML that downstream tools will fail to parse. For example, an IPv6 address or a comment character in a label corrupts the file.  
  **Remediation**: Use `gopkg.in/yaml.v3` to marshal the output struct.

- [ ] **L7 — `InsecureIgnoreHostKey()` enabled silently with no log warning** — `internal/sshutil/hostkey.go` `GetHostKeyCallback` (≈line 30) — Security — When `insecureSkip=true`, the function returns `ssh.InsecureIgnoreHostKey()` without printing any diagnostic. An operator who sets this flag by mistake (e.g., to work around a missing `known_hosts` entry) gets no warning and is silently exposed to MITM attacks on all SSH connections used for cluster bootstrap.  
  **Remediation**: Add `log.Println("WARNING: SSH host key verification disabled; susceptible to MITM attacks")` when `insecureSkip=true`.

- [ ] **L8 — `writeJobManifest` uses `text/template` with operator-supplied values; template injection possible** — `cmd/k8s-trainer/main.go` `writeJobManifest` (≈line 90) — Security — `NSName`, `Repo`, and `Image` are interpolated into a Kubernetes YAML manifest via `text/template`. If any field contains `{{` characters (e.g., a Go template string accidentally stored in a config file), the template engine panics or renders unexpected YAML.  
  **Remediation**: Use `gopkg.in/yaml.v3` to construct the manifest programmatically, or add a validation step that rejects field values containing `{{`.

- [ ] **L9 — Video job ID generated from `UnixNano()` is not collision-resistant under concurrent submissions** — `cmd/gateway/videos.go` `newVideoJob` (≈line 165) — Logic — `fmt.Sprintf("vid-%d", now.UnixNano())` produces the same ID for two goroutines that call `newVideoJob` within the same nanosecond. The second job overwrites the first in `globalVideoJobs.jobs`.  
  **Remediation**: Use `crypto/rand` or an incrementing atomic counter for job IDs.

- [ ] **L10 — Custom `contains`/`indexStr` functions duplicate `strings.Contains`/`strings.Index`** — `cmd/gateway/lora.go` (≈line 90) — Code quality — These 20-line reimplementations exist instead of simply importing `strings`. Their presence suggests a misunderstanding of the standard library, and any subtle divergence from stdlib semantics could introduce bugs.  
  **Remediation**: Replace `contains(s, substr)` with `strings.Contains(s, substr)` and delete `indexStr` and `contains`.

- [ ] **L11 — `loadNamespaces` / `loadInventory` duplicated across 7+ packages** — Multiple packages — Code quality — Variants of `loadNamespaces` appear in `cmd/dataset-build`, `cmd/modelfile-gen`, `cmd/eval-harness`, `cmd/pipeline`, `cmd/k8s-trainer`, `cmd/repo-sync`, and `cmd/rag-reindex`. The implementations are slightly different (some use `yaml.v3`, others hand-roll the parser). Inconsistencies between copies are a source of subtle parsing bugs.  
  **Remediation**: Extract a shared `internal/config` package that exports a canonical `LoadNamespaces(path string) (*NamespacesFile, error)` function.

- [ ] **L12 — No graceful HTTP shutdown in `cmd/rag` or `cmd/gateway`** — `cmd/rag/main.go` and `cmd/gateway/main.go` — Resource lifecycle — Both daemons call bare `http.ListenAndServe` with no signal handler. SIGTERM (the default Kubernetes / systemd termination signal) aborts in-flight requests mid-response. This is harmless for idempotent GET requests but may corrupt streaming responses in progress.  
  **Remediation**: Wrap in `http.Server` and implement `Shutdown` on `SIGTERM` (see M10 remediation; apply the same pattern to the gateway).

- [ ] **L13 — Prometheus `/metrics` endpoint is unauthenticated; exposes backend topology** — `cmd/gateway/main.go` `authMiddleware` (≈line 100) — Security — The middleware explicitly skips auth for `/metrics`. The endpoint exposes backend URLs, model names, health status, and request counts. In a public deployment this reveals internal infrastructure layout.  
  **Remediation**: Either require auth for `/metrics` via a separate `Authorization` header check or bind the metrics endpoint to a non-public address (e.g., `127.0.0.1:9090`).

- [ ] **L14 — Variable `nsFile` shadowed within loop body; readability hazard** — `cmd/dataset-build/main.go` `main` (≈line 95) — Code quality — The outer `nsFile *NamespacesFile` (namespace config) and the inner `nsFile *os.File` (output file) share the same name. `go vet` does not warn, but any future edit that accidentally uses the inner `nsFile` where the outer is intended (or vice versa) will silently operate on the wrong object.  
  **Remediation**: Rename the inner variable to `outFile` or `datasetFile`.

- [ ] **L15 — Zero test files across all 26 packages** — All packages — Testing — The project has no automated tests. There is no regression coverage for any of the findings in this report. `make test` in the Makefile exists but runs against empty packages. Critical functions including `pickBackend`, `diskUsagePct`, `loadInventory`, `bm25Score`, `chunkText`, and `executeBootstrapSteps` have no test coverage.  
  **Remediation**: Add table-driven unit tests for at least the identified critical-path functions. Priority: `pickBackend` (C2, M4, M5), `diskUsagePct` (M1), `chunkText` (L3), `bm25Score` (L2), inventory parsers (H2, H3, M6).

- [ ] **L16 — `isIdempotentError` swallows real failures whose output contains "already"** — `cmd/cluster-bootstrap/main.go` `isIdempotentError` (≈line 240) — Logic — The function returns `true` (treat as idempotent) if the SSH command output contains "already". A real error message like "package already downloaded but installation failed" matches and is silently ignored.  
  **Remediation**: Use more specific substring checks (e.g., `"already installed"`, `"already exists"`) or require exact matches for known idempotent patterns.

---

## Metrics Snapshot

| Metric | Value |
|--------|-------|
| Total functions | 244 |
| Total methods | 117 |
| Total functions + methods | 361 |
| Functions above complexity 15 | 10 |
| Functions above 50 lines | 24 (6.6%) |
| Avg cyclomatic complexity | 5.5 |
| Doc coverage (overall) | 48.1% |
| Doc coverage (functions) | 60.0% |
| Doc coverage (methods) | 13.9% |
| Duplication ratio | 1.30% (136 lines, 12 clone pairs) |
| Test pass rate | N/A (0 test files) |
| go vet warnings | 0 (code-level) |
| Total lines of code | 5,678 |
| Total packages | 4 (all distinct; 26 command binaries share `package main`) |

---

## False Positives Considered and Rejected

| Candidate | Reason Rejected |
|-----------|----------------|
| `gw.mu.Lock()` in `handleStatus` + `b.mu.RLock()` per backend (nested lock) | `probeAll` releases `gw.mu` before acquiring any `b.mu`; lock ordering is consistent (gw.mu → b.mu); no deadlock path exists |
| `handleListModels` iterates `gw.backends` without `gw.mu` | `gw.backends` slice is never written after `main()` initialization; concurrent read of a stable slice is safe in Go |
| `broadcast` holds `s.mu.RLock()` while sending to channels | Uses non-blocking `select { case ch <- msg: default: }`, so it never blocks while holding the read lock |
| `probeNodes` in `placer` writes to `node.Healthy` concurrently | Each goroutine owns its own `*Node` captured by value; no two goroutines share the same pointer |
| `quotaState.increment` day-rollover race | Protected by `qs.mu.Lock()` which serializes the read-modify-write of `qs.day`; no race |
| `gw.loraAdapters` map written in `reloadAdapters` under `gw.mu.Lock()` | Correctly protected; read in `resolveLoRAModel` also under `gw.mu.Lock()` |
| `wsClient.cancel` field written/read from goroutines in console-wasm | WASM is single-threaded (JavaScript event loop); no goroutine parallelism |
| `diskUsagePct` Bfree > Blocks underflow | `stat.Bfree` is the count of all free blocks including reserved; it cannot exceed `stat.Blocks` on any POSIX-compliant filesystem implementation |
| `newVideoJob` UnixNano collision as CRITICAL | Collision requires sub-nanosecond concurrency on the same core; practical risk is very low; rated LOW |
| `modelfile-gen` template injection via `SystemPrompt` | `SystemPrompt` is empty in all current code paths (never populated from config); theoretical only |
| `cloneAndIngest` in rag-ingest passes `repoURL` to `git clone` | URL is operator-supplied via flag, not user-controlled in the deployment model |

---

## Remaining Scope

All packages have been audited. No remaining scope.
