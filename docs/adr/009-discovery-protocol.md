# ADR 009 — Discovery Protocol: UDP Beacon vs. mDNS

**Status:** Accepted  
**Date:** 2026-06-03

## Context

The cluster must dynamically discover node-agent instances running on the LAN to support auto-discovery of resources without manual inventory management. This is now the **default deployment path** for the cluster. Two approaches were evaluated:

1. **Custom UDP Beacon**: Simple multicast on `239.77.0.1:9977` using stdlib `net.UDPConn`; no external dependencies.
2. **mDNS/DNS-SD**: Standards-based discovery using `github.com/grandcat/zeroconf` or similar.

## Decision

**Implement custom UDP beacon protocol (Accepted).**

- Uses stdlib `net.UDPConn` only; no new external dependencies on node-agent binary.
- Reduces attack surface and deployment friction.
- Fallback to tailnet broadcast if link-local multicast is filtered.
- **This is now the default deployment path** for the cluster.

## Rationale

### UDP Beacon (Accepted)

- **Simplicity**: Raw bytes, no zeroconf library required.
- **Stdlib only**: Reduces binary size and dependencies.
- **Fallback**: Seamlessly falls back to tailnet broadcast if multicast is unavailable.
- **Latency**: Direct UDP, < 100 ms discovery latency on local networks.

### mDNS (Alternative)

- **Standards Compliance**: RFC 6762 / DNS-SD.
- **Tool Ecosystem**: Works with `avahi-browse`, `dns-sd`, etc.
- **Limitation**: Requires external dependency; not all corporate networks allow multicast.

## Consequences

### Positive

- Minimal dependencies on node-agent binary.
- Fast discovery (< 100 ms on LAN).
- Graceful degradation to tailnet unicast.

### Negative

- Custom protocol means custom client code.
- No out-of-the-box `dns-sd` tool support.

## Implementation Notes

- Beacon emitted every 10 seconds on `239.77.0.1:9977`.
- Payload: JSON-serialized `BeaconMessage` (hostname, roles, address, port).
- Gateway joins multicast group on `--discovery=true` flag.
- Reconciliation logic in `internal/discovery/reconciler.go`.
