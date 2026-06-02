# ADR 003 — Networking: Tailscale vs WireGuard

**Status:** Accepted  
**Date:** 2026-06-01

## Context

Cluster nodes may be in different physical locations (home lab, co-lo, office).
A VPN overlay is required for secure node-to-node communication and for
exposing the gateway to authorised users without opening public firewall ports.

## Decision

**Default to Tailscale** for the node overlay network.

Raw WireGuard is documented below as an alternative.

## Rationale

### Tailscale (default)

- Zero-config mesh VPN: nodes discover each other automatically via the
  Tailscale coordination server.
- Works through NAT and CGNAT without port-forwarding.
- Built on WireGuard under the hood — same security guarantees.
- `tailscale up --auth-key=<key>` is a single command in the bootstrap
  playbook.
- Stable tailnet IP (`100.x.y.z`) used as node address in `cluster/inventory.yaml`.
- Access control lists (ACLs) restrict which users can reach the gateway.

### Raw WireGuard (alternative)

For operators who require self-hosted coordination:

- Deploy `wg-easy` or `innernet` as the coordination layer.
- Assign static `10.x.y.z` addresses; update `cluster/inventory.yaml`.
- Replace `tailscale up` in `cmd/cluster-bootstrap` with `wg-quick up wg0`.

## Consequences

- All node addresses in `cluster/inventory.yaml` are Tailscale IPs.
- Bootstrap (`cmd/cluster-bootstrap`) assumes `tailscale status --json` is
  available to verify node reachability.
- Operators switching to raw WireGuard must update both the bootstrap command
  and the inventory address scheme.
