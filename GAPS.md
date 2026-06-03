# Implementation Gaps — 2026-06-03

This report lists divergences between what the project documents/claims and what the code
actually does. Each gap maps to one or more findings in `AUDIT.md`.

## Fine-tuning datasets are built from Git metadata, not source code
- **Stated Goal**: `make train` runs the full fine-tuning pipeline; `cmd/dataset-build` turns the
  synced repository cache into JSONL training examples of repository source.
- **Current State**: `cmd/repo-sync/main.go:116-123` creates **bare** clones
  (`git clone --bare --filter=blob:none`), which contain no working tree. `cmd/dataset-build`
  `walkRepo` (`cmd/dataset-build/main.go:175-206`) walks the bare directory and only skips
  `objects`/`refs`/`logs`/dot-dirs, so it reads `config`, `HEAD`, `packed-refs`, and
  `hooks/*.sample` — never the repositories' actual files.
- **Impact**: Every training dataset on the primary path is corrupt (Git plumbing + sample
  shell hooks instead of code), silently degrading or poisoning fine-tuning.
- **Closing the Gap**: Read blobs from the bare repo via `git --git-dir <repo> ls-tree -r HEAD`
  + `git show <oid>` (or sync non-bare worktrees), and write tree-entry contents as examples.

## The documented full pipeline fails at the first stage
- **Stated Goal**: `make train` chains sync → dataset → train → convert per namespace.
- **Current State**: `cmd/pipeline/main.go:143` passes `-namespace` to `repo-sync`, but
  `repo-sync` defines no such flag (`cmd/repo-sync/main.go:64-69`), so Go's `flag` package exits
  non-zero with "flag provided but not defined: -namespace".
- **Impact**: The pipeline aborts during sync before any dataset or training work runs.
- **Closing the Gap**: Add a `-namespace` filter to `repo-sync`, or remove the unsupported
  argument from `cmd/pipeline`.

## Holdout evaluation sets are never produced
- **Stated Goal**: `cmd/eval-harness` evaluates models against `holdout.jsonl` sets created by
  `dataset-build` when a holdout ratio is set.
- **Current State**: `cmd/dataset-build/main.go:118` writes only `dataset.jsonl` and exposes no
  holdout-ratio flag; `cmd/eval-harness/main.go:251` treats a missing holdout as `nil, nil` and
  scores it as a zero-example success.
- **Impact**: Regression evaluation silently "passes" without evaluating anything.
- **Closing the Gap**: Add `-holdout-ratio` + `holdout.jsonl` output to `dataset-build`, and make
  `eval-harness` error on a missing holdout unless explicitly allowed.

## MinIO object storage and backup are documented but unimplemented
- **Stated Goal**: `cmd/rag-ingest` documents writing raw files + chunks to MinIO
  (`rag/<collection>/<hash>/`) and `--backup` exporting snapshots to `rag/snapshots/`.
- **Current State**: No MinIO/S3 client exists anywhere in the package; `backup()`
  (`cmd/rag-ingest/main.go:359-369`) only calls Qdrant `CreateSnapshot` and logs the response.
- **Impact**: Operators believe durable, off-node backups and object storage exist when they do
  not; a node loss can mean total RAG-data loss.
- **Closing the Gap**: Implement the MinIO uploads (raw files, chunks, snapshot export), or revise
  the package doc and `--backup` help text to describe Qdrant-snapshot-only behavior.

## RAG retrieval is broken on authenticated deployments
- **Stated Goal**: `cmd/rag` answers `/rag/query` and `/rag/answer` by embedding queries through
  the cluster gateway; the gateway supports API-key auth.
- **Current State**: the retrieve path's embedding call (`cmd/rag/main.go:391`) does not forward
  the caller's `Authorization` header, so a gateway configured with `GATEWAY_API_KEYS` returns 401.
  The gateway's own RAG-injection path also swallows non-2xx RAG responses (`cmd/gateway/rag.go:105`).
- **Impact**: With auth enabled, RAG retrieval fails entirely and prompts silently run without
  context.
- **Closing the Gap**: Thread `Authorization` through `retrieve`→`embed`, and check RAG HTTP
  status before decoding in the gateway injector.

## Document ingestion uses invalid Qdrant point IDs
- **Stated Goal**: `cmd/rag-ingest` upserts embedded chunks into a Qdrant collection.
- **Current State**: point IDs are `fmt.Sprintf("%s-%d", hash[:16], i)` typed as a
  `PointId_Uuid` (`cmd/rag-ingest/main.go:267`), which is not a valid UUID.
- **Impact**: Qdrant rejects the malformed IDs, so primary ingestion fails.
- **Closing the Gap**: Use a valid UUIDv5 derived from hash+index or a numeric point ID.

## Load balancing claims model-aware routing it does not perform
- **Stated Goal**: `BackendRegistry.Pick` is documented to select a backend "for the given role
  **and model**"; `BackendRecord` carries a `Models` list.
- **Current State**: all three pickers (`internal/lb/picker.go:52`, `least_queue.go:22`,
  `latency_ewma.go:29`) filter only by health and role, never by `Models`.
- **Impact**: Requests/pipeline stages can be routed to backends that do not serve the requested
  model, causing inference failures.
- **Closing the Gap**: Add a shared model filter (empty model = any) to all pickers.

## Pipeline status API always reports "completed"
- **Stated Goal**: clients submit pipelines to the gateway and poll `GET /v1/pipelines/{id}` for
  real status.
- **Current State**: `cmd/gateway/pipelines.go:69-78` returns a hardcoded
  `{"status":"completed"}` placeholder for any ID and never stores executions.
- **Impact**: Running/failed/nonexistent pipelines all appear successfully completed.
- **Closing the Gap**: Persist executions by ID at submit time, return real status, and 404 on
  unknown IDs.

## Node discovery API never reports peers
- **Stated Goal**: `cmd/node-agent` runs a UDP discovery beacon/listener and exposes peers via
  `GET /api/v1/peers`.
- **Current State**: `h.peers` is initialized empty and never populated from received beacons
  (`cmd/node-agent/main.go:125,256-263`).
- **Impact**: The discovery API always returns an empty list, defeating auto-discovery
  observability.
- **Closing the Gap**: Record listener messages into `h.peers` under `peersMu`.

## Video-to-video edits ignore the supplied video
- **Stated Goal**: `POST /v1/videos/edits` performs video→video / img→video edits from the
  provided media.
- **Current State**: `handleVideoEdits` (`cmd/gateway/videos.go:194-198`) forwards only the
  prompt, model, and image, dropping `req.Video`.
- **Impact**: Video edits run without the source video and produce wrong output with no error.
- **Closing the Gap**: Forward `req.Video` to the backend or reject unsupported video edits.

## `make down` is documented but not implemented
- **Stated Goal**: README and `Makefile` list `down` as the lifecycle target to gracefully stop
  all cluster services.
- **Current State**: `Makefile:52-53` only prints `TODO: implement cluster down (Phase 2)`.
- **Impact**: Operators following the documented lifecycle cannot stop the cluster through the
  central task runner.
- **Closing the Gap**: Implement the `down` target or relabel it as future work in README/Makefile.

## `make status` and `make sync` report success on failure
- **Stated Goal**: `status` diffs declared vs actual cluster state (documented exit code 2 on
  drift/error); `sync` reliably updates the repo cache.
- **Current State**: `cmd/status/main.go:106` ignores a failed `kubectl get nodes` and can exit 0
  reporting "No drift"; `cmd/repo-sync/main.go:92-104` logs but never propagates worker errors.
- **Impact**: Operators/CI receive false "healthy"/"in sync" signals.
- **Closing the Gap**: Exit 2 when node retrieval fails in `status`; aggregate and return worker
  errors (non-zero exit) in `repo-sync`.

## `k8s-trainer -namespaces` flag has no effect
- **Stated Goal**: the flag selects which namespaces config the training Job uses.
- **Current State**: `cmd/k8s-trainer/main.go:135` parses the flag but the Job always mounts
  `/config/namespaces.yaml` from a fixed ConfigMap.
- **Impact**: Custom namespace files are silently ignored in Kubernetes training.
- **Closing the Gap**: Mount the requested file into the Job, or remove the flag.

## README understates implemented functionality
- **Stated Goal**: README describes a scaffold where many command paths are placeholders.
- **Current State**: gateway, RAG, registry, console, cluster-bootstrap, doctor, drain, pipeline,
  node-agent, and more are substantially implemented (with the defects catalogued above).
- **Impact**: Users underestimate available functionality and miss operational constraints of the
  implemented-but-buggy commands.
- **Closing the Gap**: Refresh README feature/usage sections to distinguish implemented, partial,
  and planned commands, and link known limitations.
