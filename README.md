# github.com/opd-ai/cluster

`cluster` is a **zero-configuration**, self-hosted AI cluster with Go-first tooling and an optional Python training environment. Nodes automatically discover each other on the LAN via UDP multicast beacons—no manual inventory editing required. The root module is declared in `go.mod` as `github.com/opd-ai/cluster`, and the top-level workflow is orchestrated through `Makefile`. The codebase provides implemented command packages under `cmd/` (gateway, console, RAG, node-deploy, node-agent, and more).

---

## Description

This repository provides a **zero-conf cluster deployment** that automatically discovers and registers nodes. Each node runs `cmd/node-agent`, which broadcasts its capabilities via UDP multicast (`239.77.0.1:9977`). The gateway and other nodes automatically discover peers, eliminating manual inventory management for most deployments.

The system also supports cluster lifecycle operations, model training, serving, a web console, and RAG workflows through named `make` targets. A Python project in `python/pyproject.toml` named `cluster-training` provides PyTorch-based training dependencies.

---

## Quick Start (Zero-Configuration)

The fastest way to get a cluster running is with zero-conf deployment. On each node:

```bash
# 1. Clone and build
git clone https://github.com/opd-ai/cluster.git && cd cluster
make build

# 2. Deploy services for your node's roles (auto-detects hardware)
make deploy ROLES=chat,image-generation

# 3. Start the node agent (enables discovery)
NODE_AGENT_API_KEY=change-me
go run ./cmd/node-agent --roles chat,image-generation --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

Nodes automatically discover each other over the LAN. When the gateway (`cmd/gateway`) is started with `--discovery=true`, it joins the discovery multicast group and routes requests to discovered backends.

For a complete two-node example, see `examples/quickstart-2node/README.md`.

---

## Installation

1. Clone the repository and enter the workspace.
2. Install required toolchains and linters listed in `CONTRIBUTING.md`.
3. Optionally install pre-commit hooks.
4. Run baseline quality checks from the repository root.

```bash
git clone https://github.com/opd-ai/cluster.git
cd cluster

# Optional but recommended
pip install pre-commit
pre-commit install
```

### Requirements

The following requirements are explicitly documented in `CONTRIBUTING.md` and `python/pyproject.toml`:

- Go 1.25 or newer
- Python 3.11+
- `uv` (used for Python lint commands in the Makefile)
- `golangci-lint`, `shellcheck`, `yamllint`, `markdownlint-cli`
- Optional Python dependency groups: `image`, `dev`

The Python project metadata is:

```toml
# python/pyproject.toml
[project]
name = "cluster-training"
version = "0.1.0"
requires-python = ">=3.11"
```

---

## Usage

### Zero-Configuration Deployment (Default)

Use `make deploy` and start `cmd/node-agent` to run nodes that automatically discover each other:

```bash
# Deploy services for a chat + image-gen node (auto-discovers hardware)
make deploy ROLES=chat,image-generation

# Start node agent (broadcasts capabilities, enables discovery)
go run ./cmd/node-agent --roles chat,image-generation --address <tailnet-ip> --api-key "$NODE_AGENT_API_KEY"
```

The `node-agent` daemon broadcasts UDP beacons and serves `/api/v1/info`, `/api/v1/health`, and `/api/v1/metrics` endpoints. Other node-agents discover these peers automatically; the gateway discovers them when started with `--discovery=true`.

### Manual Inventory Configuration (Legacy)

For environments where auto-discovery is not desired, you can still configure nodes manually in `cluster/inventory.yaml`:

```bash
# Show all documented targets
make help

# Bootstrap nodes from manual inventory
make bootstrap

# Bring up the cluster (k3s control-plane + workers)
make up
```

### Build and Test Targets

```bash
# Lint all configured domains
make lint

# Run Go tests with race detector
make test

# Build cmd/* binaries into bin/
make build
```

### Lifecycle Targets

The repository defines lifecycle-oriented targets for cluster bring-up, synchronization, training, serving, console, RAG, and status diffing:

```makefile
# Makefile
deploy: ## Deploy node services for roles (zero-conf, default)
agent: ## Start node-agent for discovery
bootstrap: ## Bootstrap nodes listed in cluster/inventory.yaml (legacy)
up: ## Bring up the cluster (k3s control-plane + workers)
sync: ## Sync repo-cache and push updated datasets
train: ## Run the full fine-tuning pipeline
serve: ## Start the inference gateway locally
console: ## Start the web console server (serves WASM client)
rag: ## Start the RAG service locally
status: ## Diff declared vs actual cluster state
```

---

## Features

- **Zero-Configuration Deployment** - Nodes automatically discover each other via UDP multicast beacons. Run `make deploy` and start `cmd/node-agent` with `--api-key` (or `--open`) to join without manual inventory editing.
- **Auto-Discovery Protocol** - `cmd/node-agent` broadcasts UDP beacons on `239.77.0.1:9977` every 10 seconds; gateway and peers join the multicast group to discover new backends automatically.
- **Multi-Role Node Support** - A single host can run multiple roles simultaneously (`chat`, `image-generation`, `training`). Resource budgeting automatically partitions VRAM, CPU, and RAM.
- **Go-first project layout** - Contribution rules state that repository-owned components are written in Go, with Python reserved for training code in `python/`.
- **Centralized task runner** - `Makefile` provides one command surface for linting, testing, building, and runtime entry points.
- **Multi-language linting workflow** - Lint targets exist for Go, Python (Ruff via `uv`), shell scripts, YAML, and Markdown.
- **WASM console build hook** - `make console-wasm` defines a Go `GOOS=js GOARCH=wasm` build path and copies `wasm_exec.js` into `web/`.
- **Web console with session auth** - The console (`cmd/console`) serves a WASM client and REST API. Bootstrap assets (index.html, main.wasm, wasm_exec.js) are public to enable login page loads; all other static assets and API routes require session token authentication.
- **Python training dependency set** - `cluster-training` declares PyTorch/Transformers/TRL/PEFT-style dependencies in `python/pyproject.toml`.
- **Contributor guidance** - `CONTRIBUTING.md` documents prerequisites, workflow, branch conventions, and review expectations.

---

## Configuration

### Zero-Configuration (Default)

With zero-conf deployment, nodes discover each other automatically. Configuration is minimal:

```bash
# Deploy with auto-detected hardware
make deploy ROLES=chat

# Override specific settings
go run ./cmd/node-agent --roles chat --address 100.64.0.10 --api-key "$NODE_AGENT_API_KEY"
```

The `--discovery=true` flag enables the gateway to join the multicast group and discover backends automatically.

### Manual Inventory (Legacy)

For deployments requiring explicit inventory control, the Makefile exposes configurable variables:

- `GOFLAGS` for passing custom Go flags
- `INVENTORY` with default `cluster/inventory.yaml`
- `GOOS` and `GOARCH` for platform-sensitive behavior
- `PYTHON_DIR` (default `python`) for Python lint command context

Example override:

```bash
make up INVENTORY=cluster/custom-inventory.yaml
```

**Note:** Manual inventory and auto-discovery can coexist. Auto-discovered nodes are merged into the inventory by `internal/discovery/reconciler.go`.

---

## Contributing

See `CONTRIBUTING.md` for full contribution process. The documented workflow includes:

- Running `make lint` and `make test` before opening a PR
- Using Conventional Commits (`feat:`, `fix:`, `docs:`, etc.)
- Opening issues for non-trivial changes
- Updating `docs/` or `docs/adr/` for significant design decisions

---

## License

This project is licensed under the BSD 2-Clause License. See `LICENSE`.
