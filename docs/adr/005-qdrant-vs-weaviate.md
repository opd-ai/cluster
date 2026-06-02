# ADR 005 — Vector Store: Qdrant vs Weaviate

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The RAG service needs a vector store for embedding storage and similarity
search. Two serious candidates: **Qdrant** and **Weaviate**.

## Decision

**Default to Qdrant.**

## Rationale

### Qdrant (chosen)

- Single Go-friendly REST/gRPC API with no required schema or type system.
- Lightweight Docker image (~150 MB); fits on CPU-only nodes.
- Payload filtering (metadata-based pre-filter) without a separate metadata
  store.
- Native snapshot API for backup/restore (used by the nightly backup CronJob).
- No licensing ambiguity: Apache 2.0.
- Competitive approximate-nearest-neighbour (ANN) performance on benchmarks
  at the dataset sizes this cluster targets (< 10M vectors).

### Weaviate (rejected)

- GraphQL-first API adds a non-trivial abstraction layer.
- Ships with an embedded ML module server; heavier resource footprint.
- Self-hosted Weaviate clusters require more careful tuning for stable
  operation on heterogeneous hardware.
- Business Source License for some enterprise features (uncertainty risk).

## Consequences

- `cmd/rag` uses Qdrant's HTTP API at `http://qdrant:6333`.
- Collection management is done via Qdrant REST; no migration tooling is
  needed.
- Operators who need Weaviate can swap the `qdrant.yaml` manifest and update
  the `QDRANT_ADDR` environment variable in the RAG deployment.
