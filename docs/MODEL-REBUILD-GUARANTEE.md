# Model Rebuild Guarantee

This document explains the reproducibility guarantees for every artifact
produced by the AI cluster — LLMs, image/video generation workflows, RAG
indexes, and the WASM console — and how to recreate them from first principles.

## Summary

Given only:
1. This repository at a specific commit SHA
2. `cluster/inventory.yaml` (node list)
3. The source commits for each Ollama model (see §1)

…the entire cluster state can be recreated deterministically.

---

## 1. LLM Reproducibility

### Pinning

All Ollama model pulls are recorded in `cluster/inventory.yaml` under each
node's `models` list. Each entry must include:

```yaml
models:
  - name: llama3.2:3b
    sha256: <ollama-model-sha256>   # from `ollama show --modelfile <name>`
```

To capture the SHA of a currently-running model:

```bash
# On the node running Ollama
ollama show --modelfile llama3.2:3b | grep '^FROM' | awk '{print $2}'
```

### Rebuild

```bash
# Re-pull an exact model version from the Ollama registry
ollama pull llama3.2:3b@sha256:<sha256>
```

> **Note on CUDA non-determinism**: Inference results may differ slightly
> across GPUs due to floating-point non-associativity in parallel reductions.
> The model *weights* are byte-for-byte identical; only the runtime outputs
> are subject to hardware variance. This is expected and unavoidable.

---

## 2. Image / Video Generation Reproducibility

Image and video outputs are reproducible given:

| Input | Where stored |
|-------|-------------|
| Seed | Logged in MinIO `outputs/` object metadata (`X-Seed` header) |
| Workflow hash | SHA-256 of the ComfyUI/SwarmUI workflow JSON |
| Checkpoint SHA | `models/` bucket object etag |

### Capturing provenance at generation time

The gateway logs the seed and workflow hash for every generation request
(structured JSON, captured by Promtail into Loki). To retrieve them:

```bash
# Query Loki for generation provenance
logcli query '{app="gateway"}' \
  | jq 'select(.msg == "generation") | {seed, workflow_hash, checkpoint}'
```

### Rebuild

```bash
# Re-run a ComfyUI workflow with the same seed and checkpoint
curl -X POST http://gateway/v1/images/generations \
  -H "Authorization: ******" \
  -d '{
    "prompt": "<original prompt>",
    "seed": <original seed>,
    "model": "<checkpoint-sha>"
  }'
```

---

## 3. RAG Index Reproducibility

A RAG index is reproducible given:

| Input | Where stored |
|-------|-------------|
| Source corpus hash | SHA-256 of each ingested document (stored in Qdrant payload) |
| Embedding model SHA | `models/` bucket object etag for the embedding checkpoint |
| Chunking parameters | Logged at ingest time in structured logs |

### Rebuild

```bash
# Delete the collection and re-ingest
COLLECTION="my-collection"
kubectl -n ai-cluster exec -it deployment/rag -- \
  curl -s -X DELETE http://qdrant:6333/collections/${COLLECTION}

# Re-ingest from the original source
curl -X POST http://gateway/ingest \
  -H "Authorization: ******" \
  -d '{"source": "git", "url": "https://github.com/org/repo",
       "collection": "'"${COLLECTION}"'",
       "embedding_model_sha": "<sha>"}'
```

---

## 4. WASM Console Reproducibility

The WASM binary is reproducible given the Go toolchain version pinned in
`go.mod`:

```
go 1.25.0
```

CI builds use `GOTOOLCHAIN=local` (set in `.github/workflows/ci.yml`) to
prevent automatic toolchain upgrades.

### Rebuild

```bash
# Using the exact Go version from go.mod
GOTOOLCHAIN=local GOOS=js GOARCH=wasm \
  go build -o web/console.wasm ./cmd/console-wasm
```

The `wasm_exec.js` shim is sourced from the Go distribution at the same
pinned version:

```bash
cp "$(go env GOROOT)/misc/wasm/wasm_exec.js" web/wasm_exec.js
```

---

## 5. Version Matrix

This table must be updated whenever a component version changes:

| Component | Version / SHA | Pinned by |
|-----------|--------------|-----------|
| Go toolchain | `go 1.25.0` in `go.mod` | `go.mod` + `GOTOOLCHAIN=local` |
| k3s | See `cluster/inventory.yaml` `k3s_version` | inventory |
| Flux CD | See `cluster/flux-system/gotk-sync.yaml` | GitOps |
| cert-manager | `v1.17.2` | `cluster/overlays/production/cert-manager.yaml` |
| Prometheus | `prom/prometheus:latest` → pin to digest | kustomization |
| Grafana | `grafana/grafana:latest` → pin to digest | kustomization |
| Loki | `grafana/loki:2.9.10` | logging.yaml |
| Promtail | `grafana/promtail:2.9.10` | logging.yaml |
| Tempo | `grafana/tempo:2.7.2` | tracing.yaml |
| OTel Collector | `otel/opentelemetry-collector-contrib:0.119.0` | tracing.yaml |
| DCGM Exporter | `nvcr.io/nvidia/k8s/dcgm-exporter:3.3.5-3.4.0-ubuntu22.04` | dcgm-exporter.yaml |
| Alertmanager | `prom/alertmanager:v0.28.1` | alerting.yaml |

> **Action required**: replace `:latest` tags with digest-pinned references.
> Use `docker inspect --format='{{index .RepoDigests 0}}' <image>` or
> `crane digest <image>` to obtain the digest.
