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

# PLAN.md — Tox + Reverse-Connection Swarm Transport Layer
Version: 2026-06  
Status: Draft specification (implementation tracking enabled)

---

# 0. Overview

This document defines a **hybrid distributed transport architecture** for a NATed, infrastructure-free AI swarm system.

It combines:

- Tox P2P network as the **control-plane mesh**
- Reverse-initiated QUIC/TCP connections as the **data-plane transport**
- Ephemeral, outbound-only connectivity for all compute traffic

The system is designed to operate under the constraint:

> No externally hosted relay, STUN/TURN, or infrastructure services are required beyond optional Tox bootstrap nodes.

---

# 1. Design Goals

## 1.1 Functional Goals

- [ ] Secure device identity via cryptographic keys (Tox identity)
- [ ] NAT traversal for control-plane communication
- [ ] Zero inbound port requirements for all nodes
- [ ] Fully decentralized device onboarding
- [ ] High-throughput data transport without central relays
- [ ] Ability to dynamically form compute clusters across heterogeneous devices

## 1.2 Non-Goals

- [ ] Guaranteed global connectivity between all nodes
- [ ] Centralized orchestration service
- [ ] Cloud-hosted relay infrastructure
- [ ] Strict deterministic scheduling guarantees under all network conditions

---

# 2. System Architecture

## 2.1 Logical Layers

```
┌─────────────────────────────────────────────┐
│              Control Plane (Tox)            │
│  - identity graph                           │
│  - node discovery                           │
│  - task dispatch                            │
│  - heartbeat / health                      │
└─────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────┐
│     Connection Orchestration Layer          │
│  - reverse connection initiation            │
│  - endpoint advertisement                   │
│  - NAT-aware routing hints                  │
└─────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────┐
│         Data Plane (QUIC/TCP)               │
│  - direct node-to-node streams              │
│  - model transfer                           │
│  - inference payloads                       │
│  - dataset shards                           │
└─────────────────────────────────────────────┘
```

---

# 3. Core Principles

## 3.1 Outbound-Only Connectivity Rule

All nodes MUST:

- [ ] Establish all data connections via outbound initiation only
- [ ] Never require inbound ports for participation
- [ ] Accept connection instructions via Tox control messages

---

## 3.2 Control-Plane Authority

The Tox network is responsible for:

- [ ] Node identity and authentication
- [ ] Task assignment
- [ ] Capability advertisement
- [ ] Connection negotiation instructions

---

## 3.3 Reverse Connection Semantics

Instead of direct peer-to-peer initiation:

### Pattern A (Control → Data initiation)

```
Node A (controller)
   via Tox → Node B:
      "Initiate QUIC connection to A:ephemeral-port"
```

```
Node B (worker)
   initiates outbound QUIC → Node A
```

- [ ] All compute data flows MUST follow this pattern

---

# 4. Node Identity Model

## 4.1 Identity Definition

Each node MUST have:

- [ ] Tox public key identity (primary ID)
- [ ] Ephemeral session key (rotating)
- [ ] Capability descriptor:
  - CPU cores
  - GPU type / VRAM
  - RAM
  - network class (LAN / WAN / mobile)

---

## 4.2 Identity Binding

- [ ] Device identity MUST be bound to a Tox public key
- [ ] Identity rotation MUST be supported for compromised nodes
- [ ] Identity revocation MUST propagate via control mesh

---

# 5. Connection Lifecycle

## 5.1 Discovery Phase

- [ ] Nodes join Tox mesh
- [ ] Nodes broadcast capability advertisements
- [ ] Control plane aggregates active topology

---

## 5.2 Task Assignment Phase

- [ ] Scheduler selects worker node(s)
- [ ] Task definition sent via Tox message

---

## 5.3 Connection Negotiation Phase

Control message includes:

- target node
- ephemeral endpoint (IP:port or LAN endpoint)
- protocol (QUIC preferred, TCP fallback)
- task ID

- [ ] Worker MUST respond with ACK via Tox
- [ ] Worker MUST initiate outbound connection

---

## 5.4 Data Execution Phase

- [ ] Worker streams data to control plane or peer node
- [ ] Stream MUST be resumable on interruption
- [ ] Partial failures MUST be recoverable at task layer

---

# 6. Transport Rules

## 6.1 Control Plane Transport (Tox)

Used for:

- [ ] Scheduling messages
- [ ] Heartbeats
- [ ] Capability updates
- [ ] Connection instructions

Constraints:

- Must be low-bandwidth
- Must remain functional under NAT traversal fallback conditions

---

## 6.2 Data Plane Transport (QUIC/TCP)

Used for:

- [ ] Model weights transfer
- [ ] Inference payload streaming
- [ ] Dataset shards
- [ ] Image/video generation outputs

Constraints:

- Must be initiated outbound by worker nodes
- Must not depend on Tox relay layer
- Must support high throughput + backpressure

---

# 7. Failure Handling

## 7.1 NAT traversal failure

- [ ] If direct connection fails, worker MUST retry alternate outbound endpoint
- [ ] Control plane MUST reassign task if connection cannot be established

---

## 7.2 Node offline detection

- [ ] Heartbeat absence via Tox triggers node marking as degraded
- [ ] Tasks MUST be re-queued or migrated

---

## 7.3 Partial execution failure

- [ ] All tasks MUST be idempotent
- [ ] Task state MUST be checkpointed

---

# 8. Security Model

## 8.1 Trust model

- [ ] All nodes are authenticated via Tox public keys
- [ ] No anonymous execution permitted
- [ ] Task execution requires signed control-plane instruction

---

## 8.2 Data integrity

- [ ] All data streams MUST be cryptographically verified
- [ ] Replay protection MUST be enforced per task ID

---

## 8.3 Revocation

- [ ] Compromised nodes MUST be removable via control mesh broadcast
- [ ] Revocation MUST invalidate future connection instructions

---

# 9. Minimal Viable Implementation (MVP)

## 9.1 Required components

- [ ] Go-based Tox control mesh integration
- [ ] Node agent with:
  - Tox identity layer
  - capability advertisement
  - connection executor
- [ ] Gateway scheduler (central brain)
- [ ] QUIC data transport layer
- [ ] Reverse connection handshake protocol

---

## 9.2 MVP success condition

System is functional when:

- [ ] Two NATed devices can exchange compute tasks
- [ ] No inbound ports are required on either device
- [ ] Data transfer occurs via reverse-initiated connection
- [ ] Control plane successfully orchestrates execution lifecycle

---

# 10. Open Problems

- [ ] Optimal scheduling under unstable peer availability
- [ ] NAT classification detection (symmetric vs cone NAT)
- [ ] Connection retry heuristics without centralized relay
- [ ] Load balancing across heterogeneous compute nodes
- [ ] Secure key rotation without breaking swarm topology

---

# 11. Future Extensions (Optional)

- [ ] Opportunistic LAN acceleration layer
- [ ] Peer-assisted data caching
- [ ] Multi-swarm federation
- [ ] Local-only inference mode (no mesh participation)

---

# End of Plan
