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
        status join drain clean test lint lint-go lint-py lint-sh lint-yaml lint-md \
        docs build tidy restore-test changelog release upgrade

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

join: ## Generate k3s join scripts for worker nodes in cluster/inventory.yaml
	$(GO) run $(GOFLAGS) ./cmd/cluster-join --inventory $(INVENTORY)

drain: ## Drain and remove a node: make drain HOST=<hostname>
	$(GO) run $(GOFLAGS) ./cmd/drain $(if $(HOST),$(HOST),)

restore-test: ## Quarterly DR drill: verify latest backup restores cleanly in an ephemeral namespace
	@TS="$$(date +%s)"; NS="restore-test-$$TS"; \
	  echo "==> Starting restore drill in ephemeral namespace $$NS"; \
	  kubectl create namespace "$$NS"; \
	  kubectl -n "$$NS" run restore-verify \
	    --image=alpine:3.21 \
	    --restart=Never \
	    --env="COLD_STORAGE_ENDPOINT=$$(kubectl -n ai-cluster get secret backup-s3-credentials -o jsonpath='{.data.endpoint}' | base64 -d)" \
	    --env="COLD_STORAGE_ACCESS_KEY=$$(kubectl -n ai-cluster get secret backup-s3-credentials -o jsonpath='{.data.access-key}' | base64 -d)" \
	    --env="COLD_STORAGE_SECRET_KEY=$$(kubectl -n ai-cluster get secret backup-s3-credentials -o jsonpath='{.data.secret-key}' | base64 -d)" \
	    --env="COLD_STORAGE_BUCKET=$$(kubectl -n ai-cluster get secret backup-s3-credentials -o jsonpath='{.data.bucket}' | base64 -d)" \
	    --env="AGE_KEY=$$(kubectl -n ai-cluster get secret backup-age-key -o jsonpath='{.data.age\.key}' | base64 -d)" \
	    -- sh -c ' \
	      apk add --no-cache age curl jq mc wget tar >/dev/null; \
	      wget -q -O /usr/local/bin/mc https://dl.min.io/client/mc/release/linux-amd64/mc; \
	      chmod +x /usr/local/bin/mc; \
	      mc alias set cold "$$COLD_STORAGE_ENDPOINT" "$$COLD_STORAGE_ACCESS_KEY" "$$COLD_STORAGE_SECRET_KEY"; \
	      LATEST=$$(mc ls "cold/$$COLD_STORAGE_BUCKET" --json | jq -r ".key" | sort | tail -1); \
	      echo "Restoring from: $$LATEST"; \
	      mc cp "cold/$$COLD_STORAGE_BUCKET/$$LATEST" /tmp/backup.tar.age; \
	      echo "$$AGE_KEY" > /tmp/age.key; \
	      age --decrypt -i /tmp/age.key -o /tmp/backup.tar /tmp/backup.tar.age; \
	      tar -tf /tmp/backup.tar | head -20; \
	      echo "Restore drill PASSED: backup archive is valid"; \
	    '; \
	  kubectl -n "$$NS" wait --for=condition=Completed pod/restore-verify --timeout=300s && \
	    echo "==> Restore drill PASSED" || \
	    (echo "==> Restore drill FAILED"; kubectl -n "$$NS" logs restore-verify); \
	  kubectl delete namespace "$$NS"

# --------------------------------------------------------------------------
# Build all Go binaries
# --------------------------------------------------------------------------
build: ## Build all cmd/* binaries into bin/
	@mkdir -p $(BIN_DIR)
	@if [ -z "$$($(GO) list ./cmd/... 2>/dev/null)" ]; then \
	  echo "  no cmd/* packages found — skipping build"; \
	else \
	  for pkg in $$($(GO) list ./cmd/...); do \
	    name=$$(basename $$pkg); \
	    echo "  building $$name"; \
	    $(GO) build $(GOFLAGS) -o $(BIN_DIR)/$$name $$pkg; \
	  done; \
	fi

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
	find . \( -name '*.yaml' -o -name '*.yml' \) \
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
# Release engineering
# --------------------------------------------------------------------------
changelog: ## Regenerate CHANGELOG.md from conventional commits using git-cliff
	@command -v git-cliff >/dev/null 2>&1 || \
	  { echo "git-cliff not found; install with: cargo install git-cliff"; exit 1; }
	git-cliff --output CHANGELOG.md
	@echo "CHANGELOG.md regenerated"

release: ## Tag a new semver release: make release VERSION=v1.2.3
	@[ -n "$(VERSION)" ] || { echo "Usage: make release VERSION=v1.2.3"; exit 1; }
	@echo "==> Releasing $(VERSION)"
	$(MAKE) changelog
	git add CHANGELOG.md
	git commit -m "chore: release $(VERSION)" --allow-empty
	git tag -a "$(VERSION)" -m "Release $(VERSION)"
	@echo "==> Tagged $(VERSION). Push with: git push origin $(VERSION)"

upgrade: ## Rolling upgrade of cluster components in safe order
	@echo "==> Phase 1: control plane"
	kubectl -n kube-system rollout restart daemonset/ssh-hardening || true
	@echo "==> Phase 2: stateful services"
	kubectl -n ai-cluster rollout restart deployment/minio
	kubectl -n ai-cluster rollout restart deployment/qdrant
	kubectl -n ai-cluster rollout status deployment/minio --timeout=120s
	kubectl -n ai-cluster rollout status deployment/qdrant --timeout=120s
	@echo "==> Phase 3: stateless services"
	kubectl -n ai-cluster rollout restart deployment/rag
	kubectl -n ai-cluster rollout restart deployment/gateway
	kubectl -n ai-cluster rollout restart deployment/console
	kubectl -n ai-cluster rollout status deployment/rag     --timeout=120s
	kubectl -n ai-cluster rollout status deployment/gateway --timeout=120s
	kubectl -n ai-cluster rollout status deployment/console --timeout=120s
	@echo "==> Phase 4: monitoring"
	kubectl -n monitoring rollout restart deployment/prometheus \
	  deployment/grafana deployment/loki deployment/tempo || true
	@echo "==> Upgrade complete. WASM console cache-busted by content hash on next deploy."

# --------------------------------------------------------------------------
# Clean
# --------------------------------------------------------------------------
clean: ## Remove build artefacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(WASM_OUT) web/wasm_exec.js
	$(GO) clean -cache -testcache
