# PLAN.md — Self-Hosting AI Cluster

A reproducible, extensible 2→X device cluster for self-hosted training and inference on modest hardware (consumer GPUs, Apple Silicon, mini PCs). Each phase below ends in a working, demoable state; later phases assume earlier ones are green.

**House rule: Go-first.** Every component we own is written in Go unless physics forbids it. Training code is Python (PyTorch/Unsloth/sd-scripts have no Go equivalent). Image/video generation reuses ComfyUI + SwarmUI as external services. Everything else — gateway, placer, registry, RAG service, ingestion, console (including the browser UI via **Ebitengine → WASM**), bootstrap, drain, cache GC, doctor, probe — is pure Go. No Node.js, no SvelteKit, no Next.js, no npm, no `node_modules` in this repo.

---

## Phase 0 — Repository Scaffolding

- [x] **0.1 Repo layout.** Canonical tree: `cmd/` (every binary), `internal/`, `pkg/`, `cluster/` (k3s manifests, kustomize), `ansible/` or `scripts/bootstrap/`, `python/` (training only), `configs/`, `examples/`, `docs/`, `tools/`, `tests/`, `web/` (Ebitengine WASM client + Go embed assets), `rag/`.
- [x] **0.2 Tooling baseline.** `go.mod` (Go 1.22+, the single source of truth for almost everything), `pyproject.toml` (Python 3.11+, `uv`) scoped strictly to `python/`. No `package.json`, no `pnpm-lock.yaml`, no `node_modules` anywhere. `.editorconfig`, `.gitattributes` (LFS for sample GGUFs and a few demo sprites), `.gitignore`.
- [x] **0.3 Pre-commit & lint.** `golangci-lint` (with `gofumpt`, `revive`, `staticcheck`, `errcheck`, `gosec`), `ruff` for Python, `shellcheck`, `yamllint`, `markdownlint`. `make lint` runs them all. Zero JS linters because zero JS.
- [x] **0.4 Makefile / Taskfile.** Top-level targets: `bootstrap`, `up`, `down`, `sync`, `train`, `serve`, `console`, `console-wasm`, `rag`, `status`, `clean`, `test`, `docs`. `console-wasm` cross-compiles the Ebitengine UI with `GOOS=js GOARCH=wasm`.
- [x] **0.5 License & governance.** `LICENSE` (Apache-2.0 or MIT), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `CODEOWNERS`.
- [x] **0.6 CI skeleton.** GitHub Actions: lint, `go test ./...` with race detector, build matrix (linux/amd64, linux/arm64, darwin/arm64, **js/wasm** for the console), `actionlint` self-check. WASM artifact uploaded as a CI artifact for size tracking.
- [x] **0.7 ROADMAP.md.** Reserved strictly for speculative ideas (multi-tenant SaaS, mobile clients, on-device personalization). The ambitious-but-targeted features (speculative decoding, LoRA hot-swap, federated training, RAG, Ebitengine console, video gen) are first-class scope inside this PLAN.

## Phase 1 — Hardware Inventory & Node Bootstrap

- [x] **1.1 Inventory schema.** `cluster/inventory.yaml` per node: `hostname`, `ssh_user`, `address`, `arch`, `os`, `role` (`control`|`worker`|`both`), `accelerator` (`cuda`|`rocm`|`metal`|`cpu`), `vram_gb`, `ram_gb`, `disk_gb`, `labels`.
- [x] **1.2 Discovery helper.** `cmd/cluster-probe` (Go, distributed as a static binary): SSHes via `golang.org/x/crypto/ssh` into each host, detects GPU vendor/VRAM/driver, CPU/RAM/disk, OS, and emits a populated inventory stub.
- [x] **1.3 Node prereqs.** Idempotent bootstrap (Ansible playbook *or* `cmd/cluster-bootstrap` driving SSH directly) installing: container runtime, NVIDIA/ROCm drivers + container toolkit, `ollama`, `git`, `git-lfs`, `rsync`, Python build deps + `uv` (only on trainer/image-gen nodes).
- [x] **1.4 Mac (Apple Silicon) path.** Bootstrap profile for `darwin/arm64`: Homebrew, `ollama`, MLX runtime, Python via `uv`, no container runtime. Mac nodes still run our Go agents natively (`darwin/arm64` build).
- [x] **1.5 Hardware sanity tests.** `cmd/doctor` runs per-node checks: GPU visible, FP16/BF16 supported, disk free ≥ threshold, clock skew, MTU, outbound HTTPS. Single Go binary, no shell sprawl.

## Phase 2 — Cluster Formation & Networking

- [x] **2.1 Choose control plane.** Default to **k3s** (single binary, light footprint); document Nomad as alternative. Macs join as "external workers" via `launchd` jobs running our Go agents, *not* k3s nodes.
- [x] **2.2 Control-plane install.** Bootstrap k3s server on the designated `control` node; export kubeconfig to `cluster/kubeconfig`. Driven by `cmd/cluster-bootstrap`.
- [x] **2.3 Worker join.** `cmd/cluster-join` (Go) runs on the control node, generates one-shot tokens + a join script per worker.
- [x] **2.4 Mesh networking.** Install **Tailscale** (or headscale for fully self-hosted) for stable identities across NAT/home networks. All cluster traffic uses tailnet addresses. Our agents use the Tailscale Go client (`tsnet`) where embedding makes sense (e.g. console serving directly on the tailnet).
- [x] **2.5 DNS & service names.** CoreDNS (k3s default) + tailnet MagicDNS for Mac workers. Reserve names: `gateway.cluster`, `console.cluster`, `registry.cluster`, `storage.cluster`, `images.cluster`, `rag.cluster`.
- [x] **2.6 Node labels & taints.** `cmd/cluster-label` reads inventory and applies labels: `accelerator=cuda|metal|cpu`, `vram=24|16|8`, `role=trainer|server|imagegen|videogen|both`. Trainer taint keeps general workloads off training boxes.

## Phase 3 — Shared Storage & Artifact Cache

- [x] **3.1 Storage tiering.** `hot` (NVMe per node, model + KV cache), `warm` (network-shared: datasets, adapters, image/video checkpoints, vector indexes), `cold` (object store, snapshots).
- [x] **3.2 Shared filesystem.** **MinIO** (S3-compatible, Go-native) on the largest-disk node; replicate to a second if available. Buckets: `models/`, `datasets/`, `adapters/`, `checkpoints/` (SDXL/Flux/video/VAE/CLIP/LoRAs), `outputs/`, `rag/` (corpora + indexes), `snapshots/`, `logs/`. All our services use the official `minio-go` client.
- [x] **3.3 Per-node cache.** Mount local NVMe at `/var/lib/aicluster/cache` on every node; LRU eviction via `cmd/cache-gc` (Go).
- [x] **3.4 Repo cache.** Shared bare-clone cache (`repo-cache/`) on warm storage; nodes pull via `rsync` or `git fetch --reference`. Operated by `cmd/repo-sync` (Go, using `go-git` for in-process operations and shelling to `git` only when needed for performance).
- [x] **3.5 Model registry.** `cmd/registry` (Go) maintains `registry/models.json`: LLM bases/adapters/GGUFs and image/video checkpoints (SDXL base/refiner, Flux dev/schnell, video models, VAEs, CLIP/T5 encoders, LoRAs), each with SHA256, size, license tag, source URL. Subcommands `list`, `push`, `pull`, `verify`.

## Phase 4 — Inference Serving Layer (LLMs)

- [ ] **4.1 Per-node Ollama daemons.** Each capable node runs `ollama serve` on a tailnet-bound port; expose `/api/tags` and `/api/generate`.
- [ ] **4.2 Model placement policy.** `cmd/placer` (Go) decides which models live on which nodes given VRAM and access patterns; writes desired state to the registry; reconciles via the Ollama HTTP API.
- [ ] **4.3 Multi-device inference.** For models too large for any single node, integrate **llama.cpp RPC** (`rpc-server` on workers, `llama-server` on coordinator) behind a feature flag. Health and lifecycle managed by `cmd/placer`.
- [ ] **4.4 Inference gateway.** `cmd/gateway` (Go, `net/http` + `chi` router) implements the OpenAI-compatible API (`/v1/chat/completions`, `/v1/completions`, `/v1/models`, `/v1/embeddings`); routes by model name; native streaming via `http.Flusher` and SSE.
- [ ] **4.5 Routing logic.** Round-robin across nodes that hold the requested model; sticky sessions per `user`+`model` to preserve KV-cache; fall back to model pull on miss.
- [ ] **4.6 Speculative decoding.** Pair each "big" model with a small draft model on the same node (e.g. 7B drafts for 70B); use llama.cpp / vLLM speculative decode where available. Placer co-locates drafts with targets; gateway exposes per-model `accept_rate` and tokens/sec metrics; feature flag `speculative.enabled` per model.
- [ ] **4.7 LoRA hot-swap.** Stand up a vLLM (or llama.cpp `--lora-base` + `--lora`) backend tier serving a base with N adapters loaded at once, switchable per-request via the `model` field (`base+adapter`). Adapter manifest watched by the gateway (Go file-watcher); new adapters appear within seconds without restart. Fall back to fresh load only on cache miss.
- [ ] **4.8 Auth & quotas.** API keys per client, per-key rate limits, per-key model allow-list. Keys stored in sealed-secrets / SOPS, never in git. Implemented as gateway middleware.
- [ ] **4.9 Health checks.** Gateway probes backends every N seconds; deregisters unhealthy nodes; surfaces status at `/healthz` and `/status`.

## Phase 5 — Fine-Tuning Pipeline (Two-Stage LoRA + Federated)

- [ ] **5.1 Carry over `namespaces.yaml`.** Authoritative config for namespaces, repos, and per-stage hyperparameters at `configs/namespaces.yaml`.
- [ ] **5.2 Repo sync.** `cmd/repo-sync` (Go) clones/updates each repo into shared `repo-cache/` with shallow + filter to keep size sane.
- [ ] **5.3 Dataset builder.** `cmd/dataset-build` (Go) produces namespace-wide `dataset.jsonl` and per-repo `repos/<label>/dataset.jsonl`; respects `min_file_bytes`/`max_file_bytes`; deduplicates by content hash.
- [ ] **5.4 Namespace LoRA training.** `python/train.py --mode namespace` (Unsloth + TRL SFT) saves merged 16-bit weights and namespace GGUF. Invoked by the Go orchestrator; Python is contained to this directory.
- [ ] **5.5 Repo LoRA training.** `python/train.py --mode repo --repo <label>` trains atop the merged namespace base; skips repos below `repo_min_samples` or in `skip_repo_lora`.
- [ ] **5.6 Adapter conversion.** `tools/setup-llama-cpp.sh` ensures `convert_lora_to_gguf.py` is available; pipeline converts each PEFT adapter to GGUF.
- [ ] **5.7 Modelfile generation.** `cmd/modelfile-gen` (Go, text/template) emits per-namespace and per-repo Ollama Modelfiles with correct `FROM` / `ADAPTER` paths and quantization labels.
- [ ] **5.8 Pipeline orchestrator.** `cmd/pipeline` (Go) wires sync → dataset → train-ns → train-repo → convert → register; supports `-skip` / `-only` and exit-code 2 for "skipped repo".
- [ ] **5.9 Distributed training jobs.** Go orchestrator wraps each training stage as a Kubernetes Job (using `client-go`) with GPU resource requests; schedules onto trainer-tainted nodes; streams logs to warm storage.
- [ ] **5.10 Multi-node training.** Integrate **DeepSpeed ZeRO-3** or **FSDP** for namespaces whose merged model exceeds single-GPU VRAM; gang-scheduled via Volcano or Kubeflow Training Operator. NCCL over tailnet with documented MTU/bandwidth floor.
- [ ] **5.11 Federated training.** For nodes holding local-only data, implement a **federated LoRA** mode: each node trains a local adapter, the coordinator (`cmd/fedcoord`, Go) averages adapters (FedAvg / FedProx) per round, evaluates the merged adapter on a public holdout, iterates. Configurable rounds, local epochs, DP-noise multiplier; opt-in per node via inventory label `federated=true`.
- [ ] **5.12 Eval harness.** Hold out N files per repo before training; after, generate from namespace-only and namespace+repo models; compute exact-match / BLEU / token-overlap; emit `eval/<ns>/<repo>.json`. Fail the pipeline if repo LoRA regresses vs namespace baseline. Eval driver in Go, prompts/answers exchanged with backends via the gateway.
- [ ] **5.13 Registry publish.** On success, push merged base GGUF, repo adapters, Modelfiles, and eval reports to the registry; tag with source commit SHA. New adapters trigger the gateway hot-swap watcher (4.7).

## Phase 6 — Image & Video Generation (SwarmUI + SDXL/Flux + Video)

- [ ] **6.1 SwarmUI deployment.** Deploy **SwarmUI** as the front-end and orchestrator for image/video generation. (External service; we don't reimplement it.) Run on a node tagged `role=imagegen`; expose at `images.cluster` behind the gateway's auth proxy (Go reverse proxy in `cmd/gateway`).
- [ ] **6.2 ComfyUI backend(s).** SwarmUI manages ComfyUI as its execution backend. Install one ComfyUI instance per CUDA/ROCm-capable image-gen node; SwarmUI auto-discovers backends.
- [ ] **6.3 Multi-backend fan-out.** Configure SwarmUI to register every image-gen node so a single SwarmUI instance schedules generations across all GPUs in parallel. Tailnet addresses only.
- [ ] **6.4 Mac/MPS backend (optional).** Apple Silicon nodes with sufficient unified memory run ComfyUI with MPS as an additional SwarmUI backend; mark slow-tier so SwarmUI prefers CUDA when available.
- [ ] **6.5 Checkpoint management.** Bootstrap script seeds `checkpoints/` from MinIO into each backend's `models/` directory: SDXL base 1.0, SDXL refiner, Flux.1-dev (or schnell), required VAEs, CLIP-L / T5-XXL encoders. Use symlinks so all backends share one copy per node.
- [ ] **6.6 LoRA & embedding library.** Standard layout under `checkpoints/loras/{sdxl,flux}/` and `checkpoints/embeddings/`; SwarmUI surfaces them; registry tracks provenance/license.
- [ ] **6.7 Workflow library.** Commit a starter set of ComfyUI workflows under `configs/workflows/`: `sdxl-base-refiner.json`, `sdxl-lora.json`, `flux-dev.json`, `flux-schnell-fast.json`, `img2img.json`, `inpaint.json`, `upscale-4x.json`. SwarmUI loads them as presets.
- [ ] **6.8 OpenAI Images API shim.** Add `/v1/images/generations` and `/v1/images/edits` to `cmd/gateway`, translating to SwarmUI's HTTP API. Same API key drives text and image generation.
- [ ] **6.9 Output handling.** Generated images and metadata (prompt, seed, model, LoRAs, workflow hash) written to MinIO `outputs/<date>/<request-id>/`; thumbnails cached on hot storage for the console.
- [ ] **6.10 Quotas & safety.** Per-key daily image/video budget; configurable NSFW filter toggle (off by default in self-hosted, documented); content-tag metadata stored alongside outputs. Enforced in the Go gateway.
- [ ] **6.11 Image-gen health checks.** Gateway probes SwarmUI `/API/ListBackends` every N seconds; tracks per-backend queue depth; exposes Prometheus metrics via `prometheus/client_golang`.
- [ ] **6.12 Image LoRA training.** `python/train_image_lora.py` using `kohya_ss` / `sd-scripts` for SDXL/Flux LoRAs from a user-provided dataset; produces files that drop into the LoRA library; jobs surfaced in the console.
- [ ] **6.13 Video generation.** Add a video tier on nodes tagged `role=videogen`. Ship workflows and checkpoints for a curated set of open video models (e.g. **AnimateDiff**, **CogVideoX**, **Hunyuan Video**, **LTX-Video**, **Wan 2.x** — exact list pinned in the registry with licenses). Run inside the same ComfyUI/SwarmUI plane so scheduling, quotas, and outputs reuse Phase 6 plumbing.
- [ ] **6.14 Video API.** Extend the Go gateway with `/v1/videos/generations` (text→video) and `/v1/videos/edits` (img→video, video→video); long-running jobs return a `job_id` with polling and webhook callbacks; outputs land in `outputs/.../video.mp4` plus a preview GIF.

## Phase 7 — RAG Sidecar

- [ ] **7.1 Vector store.** Deploy **Qdrant** (Rust, but exposes a clean gRPC/HTTP API; we use the official Go client). Collections per "knowledge namespace"; snapshots backed up to MinIO `rag/`.
- [ ] **7.2 Ingestion pipeline.** `cmd/rag-ingest` (Go) accepts directories, git repos, URLs, and uploaded files; chunks (semantic + token-aware via a Go tokenizer like `tiktoken-go`), extracts metadata, writes raw + chunks to MinIO, embeds via the gateway's `/v1/embeddings`, upserts into Qdrant. Incremental re-ingest by content hash.
- [ ] **7.3 Embedding models.** Standardize on a small/fast embedding model (`bge-small-en-v1.5` or `nomic-embed-text`) served by Ollama on every node; large/multilingual variant on capable nodes; placer treats embeddings as a first-class model class.
- [ ] **7.4 Retrieval service.** `cmd/rag` (Go) exposes `/rag/query` (top-k hybrid: dense + BM25 reranked by a cross-encoder) and `/rag/answer` (retrieve → format context → call gateway chat completion → return answer + citations). BM25 implemented in pure Go (`bleve` or our own); cross-encoder reranker served as another model behind the gateway. Per-collection access controls tied to API keys.
- [ ] **7.5 Tooling integration.** Gateway recognizes a `tools: [{type:"rag", collection:"<name>"}]` field on chat completions; if set, retrieval runs server-side and inserts a system message with citations. Compatible with OpenAI tool-call semantics so existing clients work unchanged.
- [ ] **7.6 Reindex automation.** Watcher on `repo-cache/` (Go `fsnotify`) re-ingests changed repos nightly; namespaces.yaml gains an optional `rag: { collection, include, exclude }` block so the same repos that train LLMs also feed RAG.
- [ ] **7.7 Eval.** Curated QA set per collection; nightly job measures recall@k, answer faithfulness (LLM-judge), and latency; results surface in the console.

## Phase 8 — Web Console (Pure Go + Ebitengine WASM)

The console is **one Go module** with two build targets: a server binary and a WASM client. Zero JavaScript frameworks. The HTML shell is a single `index.html` (≈30 lines) whose only job is to load `wasm_exec.js` (shipped with the Go toolchain) and instantiate `console.wasm`. Everything else — layout, widgets, charts, forms, log streams, image previews — is drawn by **Ebitengine** to an HTML5 canvas.

- [ ] **8.1 Module layout.** `cmd/console` (server), `cmd/console-wasm` (client `main` for `GOOS=js GOARCH=wasm`), `internal/ui/` (shared widget library: button, list, tab, table, sparkline, log-tailer, gallery, modal — all drawn with Ebitengine primitives), `internal/uiapi/` (typed Go structs shared across server/client, marshaled with `encoding/json` or `vmihailenco/msgpack`).
- [ ] **8.2 Server.** `cmd/console` (Go, `net/http`) serves: (a) the static `index.html` + `wasm_exec.js` + `console.wasm` (all `//go:embed`-ed), (b) a JSON/WebSocket API proxying gateway, SwarmUI, RAG, and the metrics stack, (c) auth (sessions backed by API keys; OIDC SSO optional via `coreos/go-oidc`). Single static binary, no external assets.
- [ ] **8.3 Ebitengine client bootstrap.** `cmd/console-wasm/main.go` calls `ebiten.RunGame` against a top-level `App` struct that owns a router, a current "scene", and a connection to the server. Builds with `GOOS=js GOARCH=wasm go build -o web/console.wasm ./cmd/console-wasm`. Output target size ≤ 15 MB gzipped (track in CI).
- [ ] **8.4 Widget library.** Build the widgets we need from scratch in `internal/ui/`: text rendering via `golang.org/x/image/font` (embed a single open font like Inter or IBM Plex), input handling via Ebitengine's keyboard/mouse/touch APIs, scroll/clip via custom render targets. Every widget is `Update(state) -> Draw(screen)`. No DOM, no CSS, no `<input>` elements — text inputs are drawn and edited inside the canvas (clipboard via `js.Global().Get("navigator").Get("clipboard")`).
- [ ] **8.5 Transport.** Client ↔ server over a single multiplexed WebSocket (`nhooyr.io/websocket`, which works under WASM); messages are typed Go structs serialized as JSON. Server pushes live cluster state, log lines, training/job progress, and image-gen previews. REST endpoints retained for non-interactive tooling (`curl`, scripts).
- [ ] **8.6 Auth.** Session cookies backed by API keys; OIDC SSO optional (`OIDC_ISSUER` env); RBAC roles `admin`, `operator`, `user`. Cookie carried automatically on the WebSocket handshake.
- [ ] **8.7 Cluster scene.** Live page showing every node, its labels, GPU/VRAM utilization, loaded models/adapters, and current jobs. Drill-down to per-node logs and metrics. Sparklines and gauges drawn natively in Ebitengine; no chart library dependency.
- [ ] **8.8 Chat playground.** Multi-turn chat against any served model, with streaming (token-by-token render in the canvas), tool-call inspection, RAG collection selector, and side-by-side comparison of two models. Markdown is rendered to in-canvas text by a small Go markdown renderer (`yuin/goldmark` parsing → custom Ebitengine layouter).
- [ ] **8.9 Image/video studio.** Native Ebitengine UI for the common case (prompt, model, LoRA picker, seed, batch size); generated images pulled from MinIO and drawn directly on the canvas; gallery view with infinite scroll over MinIO `outputs/`. For advanced graph editing, the console **links out** to the upstream SwarmUI URL behind SSO — we don't reimplement node graphs in Ebitengine.
- [ ] **8.10 Training scene.** List namespaces and repos, kick off pipeline runs, watch live logs (canvas-rendered scrollback with search), view eval reports and regression diffs, promote/rollback adapters.
- [ ] **8.11 RAG admin.** Create collections, upload/ingest sources (drag-drop into the canvas, captured via the browser's File API through `syscall/js`), browse chunks, run test queries, view eval scores.
- [ ] **8.12 Model registry browser.** Search models/adapters/checkpoints, see SHA/size/license/source, see which nodes hold each, trigger pulls/evictions.
- [ ] **8.13 Audit log.** Every mutating action recorded in an append-only log surfaced in the console and shipped to Loki.
- [ ] **8.14 Accessibility & fallback.** Because canvas-only UIs are inherently weak on screen readers, ship a **secondary plain-HTML admin** rendered server-side via `html/template` covering the critical read-only surfaces (cluster status, audit log, registry list). Documented as the accessibility-compliant entry point. No JS framework — just Go templates and `<form>` POSTs.
- [ ] **8.15 WASM size & perf budget.** CI enforces gzipped WASM ≤ 15 MB and cold-start TTI ≤ 3 s on a baseline laptop. Use `tinygo` as a compile target only if upstream Ebitengine support is solid at the time; otherwise stay on standard `go` and accept the size hit.

## Phase 9 — Self-Deployment

- [ ] **9.1 Single-command bootstrap.** `./bootstrap` (Go binary) SSHes in, installs prereqs, brings up k3s, prints the worker join command.
- [ ] **9.2 GitOps.** Install **FluxCD** pointed at the repo's `cluster/` directory; cluster reconciles itself from git.
- [ ] **9.3 Bootstrap manifests.** All cluster services (MinIO, gateway, SwarmUI, ComfyUI backends, Qdrant, console, training operators, monitoring) defined as kustomize overlays in `cluster/`.
- [ ] **9.4 Secrets management.** SOPS-encrypted secrets in git, decrypted by Flux using an age key seeded during bootstrap; document rotation.
- [ ] **9.5 Drift detection.** `make status` (calls `cmd/status`, Go) diffs declared vs actual cluster state; CI nightly job reports drift.
- [ ] **9.6 Add-a-node flow.** Documented runbook + script: append node to inventory → `make join HOST=<name>` → node appears, gets labeled, model placer / SwarmUI backend list / RAG ingestion workers reconcile within minutes.
- [ ] **9.7 Remove-a-node flow.** `cmd/drain` (Go): cordon, evict workloads, replicate any unique adapter / vector-shard data to peers, deregister from gateway, SwarmUI, and Qdrant cluster, leave tailnet.

## Phase 10 — Observability

- [ ] **10.1 Metrics.** Prometheus + node-exporter + dcgm-exporter (NVIDIA) / Apple `powermetrics` shim. Scrape Ollama, gateway, SwarmUI, ComfyUI, Qdrant, and console `/metrics` (every Go service uses `prometheus/client_golang`; we write small exporters in Go where upstream lacks `/metrics`).
- [ ] **10.2 Dashboards.** Grafana dashboards committed as JSON: cluster overview, per-node GPU/VRAM, gateway QPS/latency, speculative decode acceptance rate, LoRA hot-swap cache hits, training job progress, federated round status, SwarmUI queue depth, video-gen long-job progress, RAG retrieval latency / recall, console request rate, **WASM bundle size & TTI trend**.
- [ ] **10.3 Logs.** Loki + Promtail (or Vector) shipping container/systemd logs to warm storage; retention policy in config.
- [ ] **10.4 Traces.** OpenTelemetry SDK (Go) in the gateway, console server, and RAG service; collector to Tempo; trace IDs surfaced in the Ebitengine console for any user request.
- [ ] **10.5 Alerts.** Alertmanager rules for: node down, GPU OOM, disk > 85%, training job failed, eval regression, federated round divergence, gateway 5xx rate, SwarmUI backend unhealthy, video job stuck, Qdrant replica lag, WASM bundle bloat.

## Phase 11 — Security & Hardening

- [ ] **11.1 SSH hygiene.** Key-only auth, fail2ban or equivalent, signed `authorized_keys` from a single source of truth.
- [ ] **11.2 Network policy.** k3s NetworkPolicies restricting cross-namespace traffic; gateway and console are the only ingresses; SwarmUI/ComfyUI/Qdrant not exposed beyond tailnet.
- [ ] **11.3 TLS everywhere.** cert-manager with an internal CA; gateway and console terminate TLS; backends mTLS.
- [ ] **11.4 Image provenance.** Pin all container images by digest; verify with cosign in CI.
- [ ] **11.5 Supply-chain.** SBOM via syft on every release; vuln scan via grype in CI. Because there's no `node_modules`, the JS supply-chain attack surface is essentially zero — only `wasm_exec.js` from the Go distribution.
- [ ] **11.6 Data governance.** Document what training/generation/RAG data leaves which node; per-namespace egress rules; deletion runbook for "forget this repo", "purge generated outputs older than N days", "drop RAG collection X".
- [ ] **11.7 Federated privacy.** Federated training jobs (5.11) enforce no-raw-data egress at the network policy level; optional DP-SGD / DP-FedAvg with documented (ε, δ) budgets.

## Phase 12 — Backup & Disaster Recovery

- [ ] **12.1 Snapshots.** Nightly snapshot of MinIO buckets, Qdrant collections, and cluster etcd to cold storage; encrypted with age. Image/video checkpoints excluded by default (re-fetchable) but configurable.
- [ ] **12.2 Restore drill.** Quarterly `make restore-test` spins up an ephemeral VM cluster and verifies a snapshot restores cleanly, including a RAG round-trip.
- [ ] **12.3 Model rebuild guarantee.** Given only the repo + `namespaces.yaml` + source commits, the pipeline can recreate every published LLM byte-for-byte (modulo CUDA nondeterminism); pin seeds and library versions. Image/video workflows reproducible given seed + workflow hash + checkpoint SHA. RAG indexes reproducible given source corpus hash + embedding model SHA. WASM console reproducible given Go toolchain version (CI pins `GOTOOLCHAIN`).

## Phase 13 — Examples, Docs, & Quickstart

- [ ] **13.1 Two-node quickstart.** `examples/quickstart-2node/` with a Vagrantfile or Multipass script that brings up two VMs, runs full bootstrap, trains a tiny model, generates a sample SDXL image, ingests a tiny RAG corpus, queries the gateway, and loads the Ebitengine console end-to-end. Should complete in < 60 minutes on a laptop.
- [ ] **13.2 Mixed-fleet example.** `examples/mixed-fleet/` showing a Linux GPU box (LLM + image + video) + a Mac mini (LLM + embeddings) + a CPU-only node (RAG ingestion) working together.
- [ ] **13.3 Architecture docs.** `docs/architecture.md` with diagrams: data flow, training flow (incl. federated), LLM inference flow (incl. speculative + hot-swap), image/video flow, RAG flow, console flow (including the Ebitengine WASM render path), failure modes.
- [ ] **13.4 Runbooks.** `docs/runbooks/` for every common op: add node, replace failed disk, rotate keys, retrain a namespace, roll back a model, add a SwarmUI backend, install a new SDXL/Flux/video checkpoint, recover Qdrant, recover from a failed federated round, debug a frozen WASM canvas.
- [ ] **13.5 Tuning guide.** `docs/tuning.md`: when per-repo LoRAs help, choosing rank/epochs, evaluating regressions, sizing VRAM for SDXL vs Flux vs video, picking schnell vs dev, draft-model selection for speculative decode, chunking strategies for RAG, WASM size-vs-feature tradeoffs.
- [ ] **13.6 ADRs.** `docs/adr/` recording every load-bearing decision: k3s vs Nomad, Ollama vs vLLM for hot-swap, Tailscale vs WireGuard, SwarmUI vs raw ComfyUI, Qdrant vs Weaviate, **Ebitengine WASM vs SvelteKit/Next.js (and the accessibility tradeoff that drove Phase 8.14)**, FedAvg vs FedProx, etc.

## Phase 14 — Release Engineering

- [ ] **14.1 Versioning.** Semver on the repo; tag every cluster release; publish a changelog generated from conventional commits.
- [ ] **14.2 End-to-end test.** Nightly CI job: provision two-node ephemeral cluster, run quickstart, assert LLM serves a known prompt (with speculative decoding enabled), SwarmUI returns a valid PNG for a known seed, video pipeline returns a valid short MP4, RAG returns expected citations, **headless browser (chromedp, Go) loads the WASM console, pixel-diffs key scenes against golden images, and exercises a chat round-trip**, then tear down.
- [ ] **14.3 Upgrade path.** `make upgrade` performs ordered rollout: control plane → workers → stateful services (MinIO, Qdrant) → stateless services (gateway, console, SwarmUI); documents version-skew policy. WASM client is cache-busted by content hash on every release.
- [ ] **14.4 Telemetry opt-in.** Optional anonymous usage ping (cluster size, hardware classes, model counts) gated behind `telemetry.enabled=false` by default.

---

## Definition of Done (per phase)

A phase is complete when: (a) every checkbox is checked, (b) `make test` passes against it (including `GOOS=js GOARCH=wasm` build for any phase that touches the console), (c) docs/runbooks exist for any new ops surface, (d) the two-node quickstart still works end-to-end, and (e) at least one ADR records the design choices made.

> `ROADMAP.md` is reserved for genuinely speculative work (multi-tenant SaaS mode, mobile clients, on-device personalization, etc.). Everything previously parked there — speculative decoding, LoRA hot-swap, federated training, Ebitengine WASM console, RAG sidecar, video gen — is in-scope for 1.0 above.
