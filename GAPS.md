# Implementation Gaps — 2026-06-03

## `make down` is documented but not implemented
- **Stated Goal**: README and Makefile list `down` as the lifecycle target to gracefully stop all cluster services.
- **Current State**: `/tmp/workspace/opd-ai/cluster/Makefile:52-53` only prints `TODO: implement cluster down (Phase 2)`.
- **Impact**: Operators following the documented lifecycle cannot stop the cluster through the central task runner.
- **Closing the Gap**: Implement the `down` target or change the README/Makefile help text to clearly label it as future work.

## Pipeline invokes an unsupported repo-sync flag
- **Stated Goal**: `make train` runs the full fine-tuning pipeline, beginning with repository synchronization for each namespace.
- **Current State**: `/tmp/workspace/opd-ai/cluster/cmd/pipeline/main.go:140-144` passes `-namespace` to `repo-sync`, but `/tmp/workspace/opd-ai/cluster/cmd/repo-sync/main.go:62-69` does not define that flag.
- **Impact**: The documented full pipeline fails at the sync stage before dataset generation or training begins.
- **Closing the Gap**: Add namespace filtering to `repo-sync` or remove the unsupported argument from `pipeline`.

## Dataset builder does not create holdout files expected by eval-harness
- **Stated Goal**: `eval-harness` says it reads holdout sets created during dataset-build when `--holdout-ratio > 0`.
- **Current State**: `/tmp/workspace/opd-ai/cluster/cmd/eval-harness/main.go:4-5` documents `holdout.jsonl`, but `/tmp/workspace/opd-ai/cluster/cmd/dataset-build/main.go:95-165` exposes no holdout ratio flag and writes only `dataset.jsonl` files.
- **Impact**: Evaluation workflows cannot run against the claimed generated holdouts without manually creating files.
- **Closing the Gap**: Add holdout splitting support to `dataset-build` or update `eval-harness` documentation to require external holdout generation.

## RAG URL ingestion is documented but non-functional
- **Stated Goal**: `cmd/rag-ingest` accepts HTTP/HTTPS URLs as sources.
- **Current State**: `/tmp/workspace/opd-ai/cluster/cmd/rag-ingest/main.go:338-352` writes the URL body to one temp path and then ingests a different `.txt` path.
- **Impact**: URL sources never reach Qdrant, so one of the documented ingestion modes is broken.
- **Closing the Gap**: Ingest the actual temp file path or create the temp file with a `.txt` suffix and pass that same path downstream.

## RAG backup claims MinIO export but only creates a Qdrant snapshot
- **Stated Goal**: `cmd/rag-ingest` comments claim `--backup` exports snapshots to MinIO.
- **Current State**: `/tmp/workspace/opd-ai/cluster/cmd/rag-ingest/main.go:359-368` calls Qdrant `CreateSnapshot` and logs the snapshot response; no MinIO client or upload path exists.
- **Impact**: Operators may believe backups are off-node when they are only Qdrant snapshots.
- **Closing the Gap**: Implement MinIO export or revise the command documentation to state the actual backup behavior.

## README still characterizes command packages as placeholders
- **Stated Goal**: README describes a scaffold where many runtime command paths are placeholders.
- **Current State**: Many command packages are implemented, including gateway, RAG, registry, console, cluster bootstrap, doctor, drain, and pipeline.
- **Impact**: Users may underestimate available functionality or miss operational constraints of implemented commands.
- **Closing the Gap**: Refresh README feature/usage sections to distinguish implemented, partial, and planned commands.

## Console server and WASM filename expectations diverge
- **Stated Goal**: `cmd/console` says it serves `main.wasm`, `wasm_exec.js`, and `index.html`.
- **Current State**: The Makefile builds `/tmp/workspace/opd-ai/cluster/web/console.wasm`, while `/tmp/workspace/opd-ai/cluster/cmd/console/main.go:127-130` publicly permits `/main.wasm`.
- **Impact**: A default build can produce a WASM filename that the server comments and public path list do not match, confusing deployment.
- **Closing the Gap**: Align the Makefile output name, HTML references, and console public asset allowlist.
