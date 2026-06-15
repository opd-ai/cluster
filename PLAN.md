# Consolidated Plan

This document consolidates the unfinished work that was previously split across
`AUDIT.md`, `GAPS.md`, `ROADMAP.md`, and the older `PLAN.md`. Completed status
reports and historical summaries were removed so this file only tracks
remaining work.

## Active Implementation Work

### Observability and Health

- [ ] Implement real node health and metrics collection in
  `cmd/node-agent/main.go` for `ProcessUp`, `ModelReady`, `VRAMUsedMB`,
  `QueueDepth`, `CPUPct`, and `MemPct`.
- [ ] Add automated coverage for the node metric collectors and any caching or
  fallback behavior they use.
- [ ] Document metric collection behavior once the implementation is complete.
- [ ] Emit `MsgGenerationEvent` messages from the console WebSocket path so the
  WASM client receives live generation updates.

### Discovery and Validation

- [ ] Add an integration test proving that two `node-agent` instances discover
  each other over multicast.
- [ ] Validate multicast discovery in a real network-capable environment when
  the sandbox cannot exercise `239.77.0.1:9977`.

### Training and Configuration Gaps

- [ ] Resolve the `cmd/k8s-trainer` `-namespaces` flag mismatch by either
  wiring the flag through to the mounted config path or removing the flag and
  documenting the fixed behavior.

### Backup and Durability

- [ ] Clarify `cmd/rag-ingest` backup behavior so CLI help and documentation
  match the current implementation.
- [ ] Decide whether long-term RAG durability should remain Qdrant-local only
  or become a future MinIO or object-storage export feature.

### Quality and Coverage

- [ ] Expand automated tests for `internal/lb`.
- [ ] Expand integration coverage for `internal/discovery`.
- [ ] Refactor the highest-complexity CLI `main()` functions where doing so
  materially improves maintainability.

## Long-Horizon Backlog

- [ ] Multi-tenant SaaS control plane for hosted cluster management.
- [ ] Mobile-native clients for cluster monitoring and lightweight inference.
- [ ] On-device personalization workflows with private adapter syncing.
- [ ] Marketplace-style model or adapter discovery and sharing.
