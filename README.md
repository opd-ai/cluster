# github.com/opd-ai/cluster

`cluster` is a repository scaffold for a self-hosted AI cluster with Go-first tooling and an optional Python training environment. The root module is declared in `/tmp/workspace/opd-ai/cluster/go.mod` as `github.com/opd-ai/cluster`, and the top-level workflow is orchestrated through `/tmp/workspace/opd-ai/cluster/Makefile`. The codebase currently emphasizes project structure, task entry points, and development conventions rather than fully implemented command packages.

---

## Description

This package defines a unified development surface for cluster lifecycle operations, model training, serving, a web console, and RAG workflows through named `make` targets. It also includes a Python project in `/tmp/workspace/opd-ai/cluster/python/pyproject.toml` named `cluster-training` for PyTorch-based training dependencies. At the current state of the repository, many runtime command paths referenced by the Makefile are placeholders, so the README documents what is explicitly configured.

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

The following requirements are explicitly documented in `/tmp/workspace/opd-ai/cluster/CONTRIBUTING.md` and `/tmp/workspace/opd-ai/cluster/python/pyproject.toml`:

- Go 1.22 or newer
- Python 3.11+
- `uv` (used for Python lint commands in the Makefile)
- `golangci-lint`, `shellcheck`, `yamllint`, `markdownlint-cli`
- Optional Python dependency groups: `image`, `dev`

The Python project metadata is:

```toml
# /tmp/workspace/opd-ai/cluster/python/pyproject.toml
[project]
name = "cluster-training"
version = "0.1.0"
requires-python = ">=3.11"
```

---

## Usage

Use the top-level Makefile as the primary interface from `/tmp/workspace/opd-ai/cluster`.

```bash
# Show all documented targets
make help

# Lint all configured domains
make lint

# Run Go tests with race detector
make test

# Build cmd/* binaries (skips when no cmd packages exist)
make build
```

The repository defines lifecycle-oriented targets for bootstrap, cluster bring-up, synchronization, training, serving, console, RAG, and status diffing:

```makefile
# /tmp/workspace/opd-ai/cluster/Makefile
bootstrap: ## Bootstrap nodes listed in cluster/inventory.yaml
up: ## Bring up the cluster (k3s control-plane + workers)
sync: ## Sync repo-cache and push updated datasets
train: ## Run the full fine-tuning pipeline
serve: ## Start the inference gateway locally
console: ## Start the web console server (serves WASM client)
rag: ## Start the RAG service locally
status: ## Diff declared vs actual cluster state
```

These targets call `go run` on `./cmd/...` entry points. If those packages are not present yet, commands depending on them will fail until implementation is added.

---

## Features

- **Go-first project layout** - Contribution rules state that repository-owned components are written in Go, with Python reserved for training code in `python/`.
- **Centralized task runner** - `/tmp/workspace/opd-ai/cluster/Makefile` provides one command surface for linting, testing, building, and runtime entry points.
- **Multi-language linting workflow** - Lint targets exist for Go, Python (Ruff via `uv`), shell scripts, YAML, and Markdown.
- **WASM console build hook** - `make console-wasm` defines a Go `GOOS=js GOARCH=wasm` build path and copies `wasm_exec.js` into `web/`.
- **Python training dependency set** - `cluster-training` declares PyTorch/Transformers/TRL/PEFT-style dependencies in `python/pyproject.toml`.
- **Contributor guidance** - `/tmp/workspace/opd-ai/cluster/CONTRIBUTING.md` documents prerequisites, workflow, branch conventions, and review expectations.

---

## Configuration

The Makefile exposes configurable variables:

- `GOFLAGS` for passing custom Go flags
- `INVENTORY` with default `cluster/inventory.yaml`
- `GOOS` and `GOARCH` for platform-sensitive behavior
- `PYTHON_DIR` (default `python`) for Python lint command context

Example override:

```bash
make up INVENTORY=cluster/custom-inventory.yaml
```

---

## Contributing

See `/tmp/workspace/opd-ai/cluster/CONTRIBUTING.md` for full contribution process. The documented workflow includes:

- Running `make lint` and `make test` before opening a PR
- Using Conventional Commits (`feat:`, `fix:`, `docs:`, etc.)
- Opening issues for non-trivial changes
- Updating `docs/` or `docs/adr/` for significant design decisions

---

## License

This project is licensed under the BSD 2-Clause License. See `/tmp/workspace/opd-ai/cluster/LICENSE`.
