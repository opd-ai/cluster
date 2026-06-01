# Implementation Gaps — 2026-06-01

This document records gaps between what the `opd-ai/cluster` project claims to do and what the code actually implements. Each gap is assessed against the README, GoDoc comments, and in-code documentation.

---

## Gap 1: Placer and FedCoord Cannot Parse Real Inventory Files

- **Stated Goal**: `cmd/placer` reads the cluster inventory and produces a model-placement plan; `cmd/fedcoord` reads the same inventory to coordinate federated training across nodes.
- **Current State**: Both commands search for `"- name:"` in the inventory YAML. The actual inventory format — as written by `cluster-bootstrap`, `cluster-join`, and `cluster-probe` — uses `"- hostname:"`. No real inventory file will match. Both tools always return zero nodes, making their output empty or nonsensical.
- **Impact**: Model placement is entirely non-functional. Federated training coordination cannot discover any nodes. Both features are effectively dead for any user following the documented workflow.
- **Closing the Gap**: Change `"- name:"` to `"- hostname:"` in `cmd/placer/main.go` (`parseInventory`) and `cmd/fedcoord/main.go` (`loadFedNodes`). Add a validation step that logs an error and exits if zero nodes are parsed from a non-empty inventory file. Consider extracting a shared inventory parser (see Gap 7).

---

## Gap 2: Gateway Load Balancing Is Not Round-Robin

- **Stated Goal**: The gateway README section and code comments describe a "round-robin" fallback for distributing inference traffic when no sticky session exists.
- **Current State**: `pickBackend` selects `candidates[0]` — the first healthy backend by slice index — on every call. There is no cycle counter, no atomic index, and no randomization. All traffic that does not hit an existing sticky session goes to the same backend indefinitely.
- **Impact**: In a multi-backend deployment, all load falls on one backend. Other backends sit idle. The performance and redundancy benefit of multiple backends is unrealized.
- **Closing the Gap**: Add an atomic counter to `Gateway` and increment it on each backend selection. Documented in AUDIT.md finding M5.

---

## Gap 3: Bootstrap Never Reports Failure

- **Stated Goal**: `cluster-bootstrap` orchestrates multi-step node setup (OS packages, k3s, GPU drivers) and is expected to exit with an error if any critical step fails.
- **Current State**: `executeBootstrapSteps` always returns `nil`. All step failures are demoted to warnings printed to stdout. `bootstrapNode` and its caller always see success regardless of how many steps failed.
- **Impact**: A user running `cluster-bootstrap` on a machine where k3s fails to install or GPU drivers fail to load receives a success exit code and a "Bootstrap complete" message. The node is silently in a broken state.
- **Closing the Gap**: Accumulate errors from non-idempotent step failures and return them from `executeBootstrapSteps`. Documented in AUDIT.md finding H1.

---

## Gap 4: Nightly RAG Re-Index Fires 60× Per Night

- **Stated Goal**: `rag-reindex` is documented as providing "nightly" re-ingestion — a single scheduled full re-ingest per day triggered at the configured UTC hour.
- **Current State**: The ticker fires every minute. During the target hour (60 minutes), the condition `t.UTC().Hour() == *nightlyHour` is true on every tick. A full re-ingest is triggered 60 times rather than once.
- **Impact**: Every repository is re-embedded 60× per night. This generates 60× the expected LLM API calls and Qdrant upsert operations, depleting API quotas and causing significant Qdrant write amplification.
- **Closing the Gap**: Track the last-triggered day and skip re-ingest if it has already run today. Documented in AUDIT.md finding H7.

---

## Gap 5: Federated Training Is Not Parallel

- **Stated Goal**: `cmd/fedcoord` is described as a "federated learning coordinator" that triggers local training across cluster nodes simultaneously, aggregates adapters, and distributes merged weights.
- **Current State**: `triggerLocalTraining` iterates nodes in a sequential `for` loop with a blocking SSH command per node. There is no parallelism. For N nodes each taking T minutes, total training time is N×T minutes rather than approximately T minutes.
- **Impact**: The defining performance characteristic of federated learning — local parallel training — is completely absent. A two-node setup takes twice as long as a single-node setup rather than approximately the same time.
- **Closing the Gap**: Use goroutines with a `sync.WaitGroup` to train all nodes concurrently. Documented in AUDIT.md finding M15.

---

## Gap 6: Console Session Tokens Have No Expiry

- **Stated Goal**: The console provides "secure multi-user access" to the cluster management UI. The login endpoint returns a "token" for subsequent API and WebSocket authentication.
- **Current State**: The token issued by `handleLogin` is the raw API key itself (acknowledged in a code comment: "For now, the session token is the API key itself"). It never expires. There is no session revocation. Any client that obtains the token has permanent, unrestricted access to all console APIs.
- **Impact**: A leaked browser session (via browser history, shared clipboard, network logging, or XSS in rendered log output) provides a permanent backdoor. Revoking access requires changing the gateway API key, which affects all services using it.
- **Closing the Gap**: Issue short-lived signed JWTs at login. The JWT payload should contain the user role and expiry; the raw API key should never leave the server. Documented in AUDIT.md finding M8.

---

## Gap 7: Inventory Schema Is Defined Five Times Inconsistently

- **Stated Goal**: The cluster inventory (`inventory.yaml`) is the single source of truth for node hostnames, addresses, roles, and hardware. Multiple tools are expected to interoperate by reading the same file.
- **Current State**: Inventory parsing is implemented independently in `cmd/cluster-bootstrap`, `cmd/cluster-join`, `cmd/cluster-probe`, `cmd/cluster-label`, `cmd/placer`, and `cmd/fedcoord` — six separate parsers. Four of them hand-roll a fragile indent-counting parser that cannot handle standard YAML. Two of them use a different field name (`name` instead of `hostname`) and are therefore broken (see Gap 1). There is no single canonical schema definition.
- **Impact**: Changes to the inventory file format must be applied to at minimum six locations. Any inconsistency between parsers causes silent data loss. The hand-rolled parsers silently drop nodes when YAML uses quoted values, anchors, or inline comments.
- **Closing the Gap**: Define an `internal/inventory` package with a canonical `Node` struct and `Load(path string) ([]Node, error)` function backed by `gopkg.in/yaml.v3`. Replace all six parsers with calls to this function. This also resolves the `hostname`/`name` mismatch in Gap 1.

---

## Gap 8: No Test Coverage for Any Feature

- **Stated Goal**: The repository includes a `Makefile` target `make test` implying the project has a test suite. The README positions this as a production-grade cluster tool for AI workloads.
- **Current State**: There are zero test files (`.go` files with `_test.go` suffix) anywhere in the repository. `make test` runs `go test ./...` against packages with no test files, which exits 0 with `[no test files]` for every package. No function, handler, or data-processing path has any automated validation.
- **Impact**: Every bug identified in this audit could have been caught by tests. Regressions introduced by fixes to H1, H2, H7, M5, M6, L3, and others will have no safety net. The project cannot be safely refactored or maintained at scale without tests.
- **Closing the Gap**: Prioritize unit tests for the highest-risk functions: `pickBackend`, `executeBootstrapSteps`, `parseInventory`, `diskUsagePct`, `chunkText`, `bm25Score`, and the YAML inventory parser. Add integration tests for the gateway's OpenAI-compatible endpoint using a mock backend.

---

## Gap 9: BM25 Retrieval Omits IDF; Stated "Hybrid BM25+Vector" Is Incomplete

- **Stated Goal**: `cmd/rag` documents a "hybrid BM25 + dense vector" retrieval strategy. BM25 is the industry-standard sparse retrieval algorithm that combines term frequency (TF) with inverse document frequency (IDF).
- **Current State**: `bm25Score` computes only the TF component with length normalization. The IDF term is absent. Without IDF, common stop-words (e.g., "the", "is", "model") score as highly as rare, information-bearing terms (e.g., "transformer", "quantization"). The retrieval function in use is TF-normalized, not BM25.
- **Impact**: Hybrid retrieval quality is degraded. Queries containing common words over-retrieve irrelevant documents. The `--bm25-weight` flag controls the contribution of a non-standard scoring function that does not match documented behavior.
- **Closing the Gap**: Build an IDF index from the retrieved Qdrant candidate set at query time (approximation), or maintain a document-frequency store in a Qdrant collection metadata field. Update the `bm25Score` signature to accept an `idf map[string]float64` argument. Documented in AUDIT.md finding L2.

---

## Gap 10: Video Job Persistence Is Not Implemented

- **Stated Goal**: `cmd/gateway/videos.go` documents "Jobs are stored in-memory (restart clears queue). Persistent storage is on the roadmap (MinIO outputs/<date>/<job-id>/)."
- **Current State**: All job state is in `globalVideoJobs` (a package-level in-memory map). A gateway restart discards all pending and completed jobs. Clients polling for a job that was running when the gateway crashed receive a 404.
- **Impact**: Long video generation jobs (up to 20 minutes) are lost on any gateway restart or deployment update. Clients have no way to recover job status. The MinIO integration described in the roadmap comment does not exist.
- **Closing the Gap**: Persist job state to a file, SQLite, or MinIO metadata object on creation and status change. On startup, reload pending jobs (those in `pending` or `running` state at shutdown should be marked `failed` since the backend job was interrupted).
