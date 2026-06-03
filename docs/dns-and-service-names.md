# DNS & Service Names — Phase 2.5

All intra-cluster traffic uses **tailnet addresses** (provided by Tailscale /
headscale).  The following logical names are reserved and mapped by the two
layers of DNS described below.

## Reserved service names

| Service name         | Default port | Component          |
|----------------------|--------------|--------------------|
| `gateway.cluster`    | 8080 (HTTP), 8443 (HTTPS) | `cmd/gateway` |
| `console.cluster`    | 8080 (HTTP), 8443 (HTTPS) | `cmd/console`  |
| `registry.cluster`   | 5000         | `cmd/registry`     |
| `storage.cluster`    | 9000 (API), 9001 (console) | MinIO |
| `images.cluster`     | 7801 (SwarmUI default), 7860 (also used in code) | SwarmUI |
| `rag.cluster`        | 6333 (HTTP), 6334 (gRPC)   | Qdrant + `cmd/rag` |

<!-- REVIEW: the SwarmUI port for `images.cluster` is inconsistent across the
codebase. The gateway swarmui-url default (cmd/gateway/images.go) and the
production network policies use 7801 (SwarmUI's real default), while the
discovery fallback and node role-port mapping (cmd/gateway/main.go:596,
cmd/node-agent, cmd/node-deploy) use 7860. Confirm the canonical port and
align code + docs. -->

## Layer 1 — CoreDNS (k3s default, Linux nodes)

k3s ships CoreDNS in the `kube-system` namespace.  Each Go service is
deployed as a Kubernetes `Service` whose `metadata.name` matches the short
hostname above.  CoreDNS resolves `<service>.cluster.svc.cluster.local` and
the short alias `<service>.cluster` via the `rewrite` plugin.

The kustomize overlay at `cluster/base/kustomization.yaml` (under the `patches:` key) injects the
`rewrite` rules so that `gateway.cluster` resolves to
`gateway.default.svc.cluster.local` (or the appropriate namespace).

## Layer 2 — Tailscale MagicDNS (all nodes, including Mac workers)

Tailscale's MagicDNS assigns a stable `<hostname>.<tailnet>.ts.net` FQDN to
every node — including Mac nodes that do not join k3s.  This means:

- Mac workers are reachable at `<hostname>.<tailnet>.ts.net` from any other
  cluster node.
- Services running on Mac nodes (Ollama on port 11434, Go agents on their
  configured ports) use the tailnet FQDN in the inventory and in `cmd/placer`
  configuration.

## headscale (fully self-hosted alternative)

To avoid a dependency on the Tailscale SaaS coordination server, deploy
[headscale](https://github.com/juanfont/headscale) on the control node
before running `make bootstrap`.  Set `TAILSCALE_LOGIN_SERVER` in the
node's environment (or pass `--login-server` to `tailscale up`) to point
clients at the headscale endpoint.

## Adding a new service

1. Create the Kubernetes `Service` and `Deployment` in `cluster/base/`.
2. Add a row to the table above.
3. If the service needs to be reachable from Mac workers (outside k3s),
   add a Tailscale `Serve` or `Funnel` config in `configs/tailscale/`.
