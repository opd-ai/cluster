# Implementation Gaps — 2026-06-02

This document records gaps between what the `opd-ai/cluster` project claims to do
(README, GoDoc package headers, in-code comments) and what the code actually
implements. Each gap is cross-referenced with the corresponding `AUDIT.md`
finding where applicable.

---

## Gap 1: Model Placement Ignores VRAM Entirely

- **Stated Goal**: `cmd/placer` "decides which node(s) should serve a given model
  based on … Available VRAM (from the `vram` node label — GiB as integer)" and
  offers a `--multi-device` split that "together satisfy the model's VRAM
  requirement" (`cmd/placer/main.go:1-13`).
- **Current State**: The inventory schema produced by `cmd/cluster-probe` and
  shipped in `cluster/inventory.yaml` uses the field name **`vram_gb`**, but
  `placer`'s parser only recognizes `case "vram":` (`cmd/placer/main.go:282`).
  Every node is parsed with `VRAM == 0`, so the GPU candidate set
  (`n.VRAM > 0`) is always empty and `buildPlan` unconditionally returns
  "no GPU nodes available; using CPU fallback".
- **Impact**: VRAM-aware placement and multi-device splitting — placer's entire
  reason to exist — never run against any real inventory. Every model is
  assigned to an arbitrary CPU node.
- **Closing the Gap**: Match `vram_gb` (accept both `vram` and `vram_gb`) in
  `parseInventory` and keep the `strconv.Atoi` conversion. Add a startup warning
  when a non-empty inventory yields zero VRAM-bearing nodes. Tracked as
  **AUDIT.md C1**.

---

## Gap 2: dataset-build Reports Success on Truncated Output

- **Stated Goal**: `cmd/dataset-build` builds per-namespace and per-repo training
  datasets (`dataset.jsonl`) consumed by the training pipeline.
- **Current State**: Writer `Flush` and file `Close` errors are discarded
  (`cmd/dataset-build/main.go:148-149,153-154`). On a short write or full disk,
  buffered JSONL lines are lost while the command logs "dataset written" and
  exits 0.
- **Impact**: Training silently consumes truncated datasets; the failure is
  invisible until model quality regresses, with no error trail.
- **Closing the Gap**: Propagate `Flush`/`Close` errors and fail the run.
  Tracked as **AUDIT.md H2**.

---

## Gap 3: Cluster Bring-Up Hides Worker-Join Failures

- **Stated Goal**: `cluster-bootstrap --up` "brings up the cluster (k3s
  control-plane + workers)" and is expected to surface failures.
- **Current State**: Worker-join errors are logged and skipped; `bringUpCluster`
  returns `nil` regardless (`cmd/cluster-bootstrap/main.go:153-160,171`).
  Control-plane install errors do propagate, but a run where all workers fail
  still prints "✓ k3s control-plane is up" and exits 0.
- **Impact**: Operators believe a multi-node cluster is healthy when only the
  control plane exists.
- **Closing the Gap**: Aggregate worker-join failures and return a non-nil error
  (or exit non-zero with a summary). Tracked as **AUDIT.md H3**.

---

## Gap 4: Console Jobs and Logs Endpoints Are Empty Shells

- **Stated Goal**: The console exposes `GET /api/jobs` ("recent JobState list")
  and `GET /api/logs?source=…` ("last N log lines") (`cmd/console/main.go:7-11`).
- **Current State**: The backing fields `s.jobs` and `s.logBuf` are read by the
  handlers but never written anywhere in the package; `pollGateway` only
  populates `s.state`. Both endpoints always return `null`/`[]`
  (`cmd/console/main.go:235-250`).
- **Impact**: Two documented console features are non-functional, with no error
  to indicate they are unimplemented.
- **Closing the Gap**: Populate `s.jobs`/`s.logBuf` from the gateway poll (or a
  dedicated source), or remove the endpoints from the documented surface until
  implemented. Tracked as **AUDIT.md H4**.

---

## Gap 5: "Hybrid BM25 + Vector" Retrieval Has No Real IDF

- **Stated Goal**: `cmd/rag` documents "hybrid BM25 + dense vector" retrieval.
- **Current State**: `bm25Score` computes `df` from the single candidate
  document (always 0 or 1) with a hardcoded `N = 1000`, so the IDF term is a
  constant for every matched term and never down-weights common words
  (`cmd/rag/main.go:71-85`). The function is effectively TF-with-length-
  normalization; it is hedged in-code as "BM25-like … simplified".
- **Impact**: Retrieval ranking quality is degraded; queries with common words
  are not penalized as standard BM25 would. The `--bm25-weight` flag tunes a
  non-standard score.
- **Closing the Gap**: Maintain a corpus document-frequency store and supply
  real per-term `df`, or update the documentation to describe TF-only scoring.
  Tracked as **AUDIT.md M5**.

---

## Gap 6: RAG Collection ACL Fails Open for Unlisted Keys

- **Stated Goal**: `cmd/rag` supports a per-key `collection-acl` restricting
  which collection each API key may query.
- **Current State**: When an ACL is configured but a valid key is not listed in
  it, `collectionAllowed` returns `true` and grants access to any collection
  (`cmd/rag/main.go:430`).
- **Impact**: A key the operator simply forgot to add to the ACL bypasses the
  restriction entirely — the control fails open and is undocumented.
- **Closing the Gap**: Deny keys absent from a configured ACL (fail closed) and
  document the semantics. Tracked as **AUDIT.md M4**.

---

## Gap 7: Per-Repo Modelfiles Reference a Directory Instead of a GGUF

- **Stated Goal**: `cmd/modelfile-gen` renders Ollama `Modelfile`s whose `FROM`
  is a GGUF path or an Ollama tag.
- **Current State**: The per-repo `FROM` is computed from
  `…/namespace/merged` — a directory — rather than a `model.gguf` file
  (`cmd/modelfile-gen/main.go:151,157`), unlike the namespace Modelfile which
  correctly uses `…/namespace/model.gguf`. When the `merged` directory exists,
  the rendered `FROM` is an unusable directory path.
- **Impact**: Repo-level model builds produce invalid Modelfiles whenever the
  namespace merged directory is present.
- **Closing the Gap**: Use the namespace GGUF file (or `merged/model.gguf`)
  consistently. Tracked as **AUDIT.md M1**.

---

## Gap 8: Video Jobs Have No Persistence or Eviction

- **Stated Goal**: `cmd/gateway/videos.go` states "Jobs are stored in-memory
  (restart clears queue). Persistent storage is on the roadmap."
- **Current State**: `globalVideoJobs` is an in-memory map that is only ever
  inserted into — there is no eviction, TTL, or size cap
  (`cmd/gateway/videos.go:62-70`). State is also lost on restart (a 404 for
  in-flight jobs), as the comment acknowledges.
- **Impact**: (a) long video jobs are lost on any gateway restart; (b) the map
  grows unbounded for the process lifetime — a slow memory leak driven by
  authenticated requests.
- **Closing the Gap**: Add a retention sweep / LRU cap now (addresses the leak,
  **AUDIT.md M2**); persist job metadata (file/SQLite/MinIO) to address restart
  durability per the roadmap.

---

## Gap 9: No Automated Test Coverage Anywhere

- **Stated Goal**: The `Makefile` provides `make test` (`go test -race ./...`),
  implying an existing test suite, and the README positions the project as
  production tooling for AI workloads.
- **Current State**: There are **zero** `_test.go` files in the repository;
  `go test -race ./...` reports `[no test files]` for every one of the 31
  packages.
- **Impact**: None of the bugs in `AUDIT.md` (C1, H2, H3, H4, the inventory
  field-name mismatch, the recall metric, etc.) would be caught by tests, and
  there is no regression safety net for any fix. The audit's dynamic
  race/panic evidence is necessarily absent.
- **Closing the Gap**: Add unit tests for the highest-risk pure functions first
  — `parseInventory` (placer/fedcoord), `bm25Score`, `recallHit`, `pickBackend`,
  `parseSizeStr`, `executeBootstrapSteps` — and handler tests for the gateway,
  rag, and console HTTP surfaces using mock backends.

---

## Notes on Previously-Reported Gaps Now Closed

The following gaps from the prior (2026-06-01) report have been verified as
resolved in the current code and are **not** re-reported:

- **Placer/fedcoord inventory `- name:` vs `- hostname:`** — both now parse
  `hostname:` (`cmd/placer/main.go:264`, `cmd/fedcoord/main.go:263`). *(Note:
  the residual `vram` vs `vram_gb` mismatch is a distinct, still-open bug — see
  Gap 1.)*
- **Gateway "round-robin" actually pinned to `candidates[0]`** — now uses an
  atomic counter (`cmd/gateway/main.go:536`).
- **Nightly RAG re-index firing 60×/night** — now guarded by a `lastNightlyDay`
  day key in `cmd/rag-reindex`.
- **Federated training sequential despite "parallel" claim** — `fedcoord`
  `triggerLocalTraining` now runs nodes concurrently with `sync.WaitGroup`
  (`cmd/fedcoord/main.go:152-189`).
- **Console session token == raw, non-expiring API key** — now a 32-byte
  `crypto/rand` token with a 24h TTL and pruning (`cmd/console/main.go:40,
  173-223`).
- **`executeBootstrapSteps` always returns nil** — now returns
  `errors.Join(errs...)` (`cmd/cluster-bootstrap/main.go:504-523`).
