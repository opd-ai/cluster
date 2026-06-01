# ============================================================================
# AI Cluster — top-level Makefile
# ============================================================================

SHELL       := /bin/bash
.DEFAULT_GOAL := help

# --------------------------------------------------------------------------
# Variables
# --------------------------------------------------------------------------
MODULE      := github.com/opd-ai/cluster
GO          := go
GOFLAGS     ?=
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)

BIN_DIR     := bin
DIST_DIR    := dist

# WASM output
WASM_OUT    := web/console.wasm
WASM_EXEC   := $(shell $(GO) env GOROOT)/misc/wasm/wasm_exec.js

# Python tooling
UV          := uv
PYTHON_DIR  := python

# Cluster inventory
INVENTORY   ?= cluster/inventory.yaml

.PHONY: help bootstrap up down sync train serve console console-wasm rag \
        status clean test lint lint-go lint-py lint-sh lint-yaml lint-md \
        docs build tidy

# --------------------------------------------------------------------------
# Help
# --------------------------------------------------------------------------
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' | \
	  sort

# --------------------------------------------------------------------------
# 0.4 Top-level lifecycle targets
# --------------------------------------------------------------------------
bootstrap: ## Bootstrap nodes listed in cluster/inventory.yaml
	$(GO) run $(GOFLAGS) ./cmd/cluster-bootstrap --inventory $(INVENTORY)

up: ## Bring up the cluster (k3s control-plane + workers)
	$(GO) run $(GOFLAGS) ./cmd/cluster-bootstrap --inventory $(INVENTORY) --up

down: ## Gracefully stop all cluster services
	@echo "TODO: implement cluster down (Phase 2)"

sync: ## Sync repo-cache and push updated datasets
	$(GO) run $(GOFLAGS) ./cmd/repo-sync

train: ## Run the full fine-tuning pipeline
	$(GO) run $(GOFLAGS) ./cmd/pipeline

serve: ## Start the inference gateway locally
	$(GO) run $(GOFLAGS) ./cmd/gateway

console: ## Start the web console server (serves WASM client)
	$(GO) run $(GOFLAGS) ./cmd/console

console-wasm: $(WASM_OUT) ## Cross-compile the Ebitengine UI to WASM
	@echo "WASM built: $(WASM_OUT)"
	@cp -f $(WASM_EXEC) web/wasm_exec.js
	@echo "wasm_exec.js copied to web/"

$(WASM_OUT): $(shell find cmd/console-wasm internal/ui internal/uiapi -name '*.go' 2>/dev/null)
	GOOS=js GOARCH=wasm $(GO) build $(GOFLAGS) -o $(WASM_OUT) ./cmd/console-wasm

rag: ## Start the RAG service locally
	$(GO) run $(GOFLAGS) ./cmd/rag

status: ## Diff declared vs actual cluster state
	$(GO) run $(GOFLAGS) ./cmd/status

# --------------------------------------------------------------------------
# Build all Go binaries
# --------------------------------------------------------------------------
build: ## Build all cmd/* binaries into bin/
	@mkdir -p $(BIN_DIR)
	@for pkg in $$($(GO) list ./cmd/...); do \
	  name=$$(basename $$pkg); \
	  echo "  building $$name"; \
	  $(GO) build $(GOFLAGS) -o $(BIN_DIR)/$$name $$pkg; \
	done

# --------------------------------------------------------------------------
# Testing
# --------------------------------------------------------------------------
test: ## Run all Go tests with race detector
	$(GO) test -race -count=1 ./...

# --------------------------------------------------------------------------
# Linting (0.3)
# --------------------------------------------------------------------------
lint: lint-go lint-py lint-sh lint-yaml lint-md ## Run all linters

lint-go: ## Run golangci-lint
	golangci-lint run ./...

lint-py: ## Run ruff on python/
	cd $(PYTHON_DIR) && $(UV) run ruff check . && $(UV) run ruff format --check .

lint-sh: ## Run shellcheck on all shell scripts
	find . -name '*.sh' -not -path './.git/*' | xargs -r shellcheck --severity=warning

lint-yaml: ## Run yamllint on all YAML files
	find . -name '*.yaml' -o -name '*.yml' \
	  | grep -v '.git' \
	  | grep -v 'cluster/kubeconfig' \
	  | xargs -r yamllint -c .yamllint.yml

lint-md: ## Run markdownlint on all Markdown files
	find . -name '*.md' -not -path './.git/*' \
	  | xargs -r markdownlint --config .markdownlint.json

# --------------------------------------------------------------------------
# Dependency management
# --------------------------------------------------------------------------
tidy: ## go mod tidy
	$(GO) mod tidy

# --------------------------------------------------------------------------
# Documentation
# --------------------------------------------------------------------------
docs: ## Generate documentation (placeholder)
	@echo "TODO: generate docs (Phase 13)"

# --------------------------------------------------------------------------
# Clean
# --------------------------------------------------------------------------
clean: ## Remove build artefacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(WASM_OUT) web/wasm_exec.js
	$(GO) clean -cache -testcache
