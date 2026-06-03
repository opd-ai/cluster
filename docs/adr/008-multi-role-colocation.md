# ADR 008 — Multi-Role Colocation: Flexible Node Role Assignments

**Status:** Proposed  
**Date:** 2026-06-03

## Context

The inventory schema currently represents each cluster node with a single `role` string (e.g., `control` or `worker`). This is insufficient for deployments where a single physical host runs multiple services simultaneously — for example, a single node hosting both a chat model (Ollama) and an image generation service (SwarmUI), or a developer workstation running training code alongside inference.

## Decision

**Extend the inventory `Node` schema to support multiple roles per host while maintaining backward compatibility.**

- Add a `Roles []string` field to represent all active roles on a node.
- Retain the deprecated `role` field for one release to allow inventory files to migrate gradually.
- Implement `PrimaryRole()` and `EffectiveRoles()` accessors for graceful degradation.
- Extend `ServiceBinding` and `VRAMBudget` to track port assignments and resource allocation per role.

## Rationale

### Problem: Single-Role Limitation

- Current schema forces operator choice: a high-end node must be labeled either `control`, `worker`, `chat`, or `training`, even though it may have sufficient resources to run multiple roles.
- This leads to resource waste and operational friction.

### Solution: Multi-Role + Coexistence

1. **Backward Compatibility**: Existing single-role inventory files continue to work. If only `role` is set, internal code treats it as a one-element `Roles` list via `EffectiveRoles()`.
2. **Service Binding**: Each role is bound to a distinct port to avoid conflicts. For example, on a single host:
   - Role `chat` runs Ollama on port 11434
   - Role `image-generation` runs SwarmUI on port 7860
   - Role `training` reserves a port for its HTTP health endpoint
3. **Resource Budget**: `VRAMBudget` is a `map[role]int` allowing granular control over GPU allocation across co-located roles.

## Consequences

### Positive

- Single node can serve multiple workloads efficiently.
- Inventory YAML is more expressive and operator-friendly.
- Gradual migration path: old inventory files work without modification.

### Negative (to address in Phase 1+)

- Deployment tooling must be smarter: `cmd/node-deploy` must reconcile role lists with systemd/launchd unit file configuration.
- Load balancer and discovery logic must understand multi-role nodes and route by (role, model) pair.
- Testing complexity: integration tests must verify resource isolation across co-located roles.

## Implementation Notes

- The change is introduced in Phase 0 (schema) and enabled gradually through Phase 1+ (deployment tooling).
- No breaking changes to existing command interfaces.
- Gateway `discoverBackends()` updated to parse the new schema and expose roles as metadata.
