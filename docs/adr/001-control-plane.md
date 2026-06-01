# ADR 001 — Control Plane: k3s

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The cluster needs a container orchestration layer to schedule inference, training, and
service workloads across heterogeneous nodes (CUDA, ROCm, Apple Silicon, CPU-only).
Two realistic candidates were evaluated: k3s and Nomad.

## Decision

**Default to k3s.**

Macs join as "external workers" via `launchd` jobs running our Go agents — they are not
k3s nodes.  Nomad is documented below as a fully supported alternative.

## Rationale

### k3s (default)

- Single static binary (< 70 MB) — no external etcd, no separate controller-manager;
  trivially installable via a one-line curl on any node that `cmd/cluster-bootstrap`
  can reach.
- Ships CoreDNS, Traefik ingress, and flannel networking by default — less to wire up.
- Native Kubernetes API; `client-go` works without adapters.
- GPU device-plugin support is well-documented for NVIDIA/ROCm; Apple Silicon nodes
  simply do not join k3s (see below).
- Battle-tested on Raspberry Pi and home-lab hardware at exactly the resource profile
  this cluster targets.

### Nomad (alternative)

Nomad is documented as an alternative for operators who prefer a simpler job-scheduler
model rather than Kubernetes:

- Lighter scheduling model: jobs are HCL files, not YAML manifests + CRDs.
- First-class support for non-containerised workloads (exec, raw_exec) — useful for
  `ollama` on nodes where container runtimes are absent.
- Vault and Consul integration for secrets and service discovery without Kubernetes
  extensions.
- To use Nomad, replace Phase 2 bootstrap steps with `nomad agent -dev` or a
  production Nomad cluster, and substitute `client-go` calls in `cmd/pipeline` and
  `cmd/placer` with the Nomad Go API (`github.com/hashicorp/nomad/api`).

### Apple Silicon ("external workers")

Mac nodes run our Go agent binaries natively under `launchd`.  They do *not* join k3s:

- No container runtime is present (Docker Desktop is optional and not required).
- Inference is served by `ollama` on the tailnet address; `cmd/placer` treats the node
  as an Ollama-native backend, not a k3s pod.
- The bootstrap profile (Phase 1.4) already handles the Mac-specific setup.
- Mac nodes appear in `cluster/inventory.yaml` with `os: darwin`; `cmd/cluster-join`
  skips them automatically.

## Consequences

- `cmd/cluster-bootstrap --up` installs k3s server on the `control` node and joins
  Linux worker nodes.
- `cluster/kubeconfig` is created (and gitignored) after a successful control-plane
  install.
- Mac nodes and any node labelled `os: darwin` are never issued a k3s join token.
- If the operator switches to Nomad, the Makefile `up` / `down` targets and the
  `cmd/cluster-bootstrap --up` path need to be retargeted — document this in
  `docs/runbooks/control-plane-swap.md` before doing so.
