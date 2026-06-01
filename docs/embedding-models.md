# Embedding Models

## Standard Embedding Models

The cluster standardizes on two embedding model tiers served by Ollama:

| Tier | Model | Dimensions | Notes |
|------|-------|-----------|-------|
| **Small / Fast** | `nomic-embed-text` | 768 | Default; runs on every GPU node |
| **Large / Multilingual** | `bge-m3` | 1024 | Scheduled on nodes with ≥ 12 GB VRAM |

`nomic-embed-text` is the default for all `cmd/rag-ingest` and `cmd/rag`
operations unless overridden by `--embedding-model`.

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

The Qdrant collection vector dimension is set at creation time.  Use the
default value (`768`) for `nomic-embed-text`.  If you switch to `bge-m3`,
create a separate collection with `--vector-dim 1024`.

## Usage in rag-ingest / rag

```bash
# Default (nomic-embed-text, 768-dim)
rag-ingest --collection my-kb --dir ./docs --gateway-url http://gateway:8080

# Large multilingual model
rag-ingest --collection my-kb-m3 --embedding-model bge-m3 --vector-dim 1024 \
           --dir ./docs --gateway-url http://gateway:8080
```
