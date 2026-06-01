# Contributing to cluster

Thank you for taking the time to contribute! Please read this guide before
opening issues or pull requests.

## House Rules

- **Go-first.** Every component we own is written in Go unless physics
  forbids it. Training code lives in `python/` (PyTorch/Unsloth/sd-scripts
  have no Go equivalent). Image/video generation reuses ComfyUI + SwarmUI as
  external services. Everything else — gateway, placer, registry, RAG service,
  ingestion, console (Ebitengine → WASM), bootstrap, drain, cache GC, doctor,
  probe — is pure Go.
- **No Node.js.** No `package.json`, no `node_modules`, no npm/pnpm/yarn
  anywhere in this repo.
- **No new dependencies without discussion.** Open an issue first so we can
  evaluate alternatives and licensing impact.

## Prerequisites

- Go 1.22 or newer (install via <https://go.dev/dl/>)
- `golangci-lint` — `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- Python 3.11+ with `uv` — only needed if touching `python/`
- `shellcheck`, `yamllint`, `markdownlint-cli` — for shell/YAML/Markdown linting
- `pre-commit` (optional but recommended) — `pip install pre-commit && pre-commit install`

## Development Workflow

```bash
# Clone
git clone https://github.com/opd-ai/cluster.git && cd cluster

# Install pre-commit hooks (optional)
pre-commit install

# Lint everything
make lint

# Run tests with race detector
make test

# Build all binaries
make build

# Cross-compile the Ebitengine WASM console
make console-wasm
```

## Branch and Commit Conventions

- Branch names: `<type>/<short-description>` — e.g. `feat/gateway-routing`,
  `fix/placer-vram-overflow`, `docs/adr-ollama-vs-vllm`.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`, `ci:`.
- Keep commits focused; squash noise before opening a PR.

## Pull Request Process

1. Open an issue first for anything non-trivial.
2. Fork, branch off `main`, implement, then open a PR.
3. All CI checks must pass (`lint`, `test`, build matrix).
4. Require at least one review from a `CODEOWNERS` maintainer.
5. Update `docs/` and/or `docs/adr/` when introducing load-bearing design
   decisions.

## Code Style

- Go: `gofumpt` + `golangci-lint` config at `.golangci.yml`. Run
  `gofumpt -l -w .` before committing.
- Python: `ruff` (see `python/pyproject.toml`).
- Shell: POSIX-compatible; `shellcheck` must be clean.
- YAML: `yamllint` with `.yamllint.yml`.
- Markdown: `markdownlint` with `.markdownlint.json`.

## Testing

- Every public Go package must have at least a smoke test.
- Integration tests live in `tests/`; unit tests are co-located (`_test.go`).
- Tests run with `-race`; do not suppress the race detector.

## Security

Please read [SECURITY.md](SECURITY.md) before reporting vulnerabilities.

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
All contributors are expected to uphold these standards.
