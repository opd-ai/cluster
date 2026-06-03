# Embedding Models

## Standard Embedding Models

The cluster standardizes on two embedding model tiers served by Ollama:

| Tier | Model | Dimensions | Notes |
|------|-------|-----------|-------|
| **Small / Fast** | `nomic-embed-text` | 768 | Default; runs on every GPU node |
| **Large / Multilingual** | `bge-m3` | 1024 | Scheduled on nodes with ≥ 12 GB VRAM |

`nomic-embed-text` is currently the embedding model used by all `cmd/rag-ingest`
and `cmd/rag` operations; it is hard-coded in the embeddings request
(see `cmd/rag-ingest/main.go`).
<!-- REVIEW: docs previously described an `--embedding-model` override flag, but
cmd/rag-ingest does not define one (the model is fixed to nomic-embed-text).
Confirm whether per-collection model selection is planned. -->

## Node Placement

The model placer (`cmd/placer`) treats embeddings as a first-class model
class with the following rules:

```yaml
# Example placer config snippet
model_classes:
  - name: embedding
    models: [nomic-embed-text, bge-m3]
    placement:
      nomic-embed-text:
        min_vram_gb: 4
        prefer_nodes: []   # any node
      bge-m3:
        min_vram_gb: 12
        prefer_nodes: [role=gpu-large]
```

## Installing via Ollama

```bash
ollama pull nomic-embed-text
ollama pull bge-m3            # large nodes only
```

## Vector Dimensions

The Qdrant collection vector dimension is set at creation time and is derived
from the embedding model's output length (`768` for `nomic-embed-text`).
<!-- REVIEW: there is no `--vector-dim` flag on cmd/rag-ingest; collection
dimension is determined by the embedding response. The bge-m3 (1024-dim)
workflow below assumes model/dimension selection that is not yet implemented. -->

## Usage in rag-ingest / rag

```bash
# Default (nomic-embed-text, 768-dim)
rag-ingest --collection my-kb --dir ./docs --gateway-url http://gateway:8080
```
<!-- REVIEW: a bge-m3 example previously used non-existent `--embedding-model`
and `--vector-dim` flags; removed pending implementation of model selection. -->
