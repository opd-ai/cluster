# Runbook: Add a Node

This runbook describes how to add a new node to a running cluster. The **recommended approach** is zero-configuration deployment, where nodes automatically discover each other. Manual inventory configuration is also supported for environments that require explicit control.

---

## Prerequisites

- The new node meets hardware prerequisites (see `docs/ollama-daemon-setup.md`).
- Tailscale is installed and the node is on the same tailnet as existing nodes.
- For k3s integration: SSH access to the new node (key-based auth).

---

## Zero-Configuration (Recommended)

With zero-conf deployment, nodes automatically discover each other via UDP multicast beacons.

### 1. Deploy services on the new node

```bash
# On the new node
git clone https://github.com/opd-ai/cluster.git && cd cluster
make build

# Deploy services for the node's roles (auto-detects hardware)
make deploy ROLES=chat,image-generation
```

The `node-deploy` command will:
1. Detect hardware capabilities (GPU, VRAM, RAM, CPU).
2. Apply resource budgets using `internal/autotuner`.
3. Install systemd units (Linux) or launchd plists (macOS).

### 2. Start the node agent

```bash
# Start the node agent (broadcasts discovery beacons)
NODE_AGENT_API_KEY=change-me
go run ./cmd/node-agent --roles chat,image-generation --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

The node-agent will:
1. Start broadcasting UDP beacons every 10 seconds on `239.77.0.1:9977`.
2. Serve HTTP endpoints at `:9977` (`/api/v1/info`, `/api/v1/health`, `/api/v1/metrics`).

### 3. Verify discovery

The gateway and other node-agents will automatically discover the new node. Verify:

```bash
# From any node in the cluster
curl http://localhost:8080/v1/models | jq '.data[].id'
# Should include models from the new node

# Or check the node-agent directly
curl -H "Authorization: ******" \
  http://<new-node-address>:9977/api/v1/info | jq
```

The node is now part of the cluster and eligible for request routing.

---

## Manual Inventory Configuration (Legacy)

For environments where auto-discovery is not desired, you can manually configure nodes.

### 1. Append the new node to the inventory

Edit `cluster/inventory.yaml` and add an entry under `nodes:`:

```yaml
nodes:
  # ... existing nodes ...
  - hostname: worker-3
    ssh_user: ubuntu
    address: 100.64.0.13      # tailnet address
    arch: amd64
    os: linux
    role: worker
    accelerator: cuda
    vram_gb: 16
    ram_gb: 32
    disk_gb: 512
    labels:
      workload: server
      zone: primary
```

Commit and push the change so FluxCD picks up the updated inventory ConfigMap
if you are using GitOps.

---

### 2. Bootstrap the new node's prerequisites (manual path)

```bash
make bootstrap
# or equivalently:
go run ./cmd/cluster-bootstrap --inventory cluster/inventory.yaml
```

This installs the container runtime, GPU drivers, Ollama, Git LFS, Tailscale, and
Python build dependencies (trainer nodes only) on the new node.

---

### 3. Join the node to the k3s cluster (manual path)

```bash
mkdir -p /tmp/join-scripts
go run ./cmd/cluster-join \
  --inventory cluster/inventory.yaml \
  --script /tmp/join-scripts
scp /tmp/join-scripts/worker-3-join.sh worker-3:/tmp/worker-3-join.sh
ssh worker-3 'sudo sh /tmp/worker-3-join.sh'
```

`cluster-join` will:
1. SSH to the k3s control node to fetch a fresh one-shot join token.
2. Generate a per-worker k3s join script for each Linux worker in the inventory.
3. Write those scripts to the directory passed with `--script`.

---

### 4. Apply node labels (manual path)

```bash
go run ./cmd/cluster-label --inventory cluster/inventory.yaml
```

This applies the `accelerator`, `vram`, `role`, and any custom labels defined
in `inventory.yaml` to the new k3s node.

---

### 5. Verify

```bash
make status
# Expected: "✓ No drift detected."
go run ./cmd/doctor --host worker-3 --remote
# Expected: all checks PASS
```

---

### 6. Model placer reconciliation

The model placer runs automatically (or can be triggered manually):

```bash
go run ./cmd/placer --inventory cluster/inventory.yaml <model-name>
```

New nodes become eligible for model placement immediately after step 4.

---

### 7. SwarmUI backend (image-gen nodes only)

For nodes with `role=imagegen`, register the new ComfyUI backend in SwarmUI:
1. Open the SwarmUI admin panel at `https://images.cluster`.
2. Go to **Server** → **Backends** → **Add Backend**.
3. Enter the new node's tailnet address and ComfyUI port (default: 8188).

---

### Rollback (Zero-Conf)

For zero-conf nodes, stop the `cmd/node-agent` process (or stop whichever
service manager entry you created locally):

```bash
# On the node
pkill -f 'cmd/node-agent'  # if running directly from go run/bin
```

The gateway will stop receiving fresh beacons. Routing health updates happen via
periodic backend probes (default interval: 15s).

### Rollback (Manual Inventory)

If the join fails or the node misbehaves:

```bash
# Cordon and drain via kubectl
kubectl cordon worker-3
kubectl drain worker-3 --ignore-daemonsets --delete-emptydir-data

# Remove from k3s
kubectl delete node worker-3

# Remove from inventory (revert your edit to cluster/inventory.yaml)
```

---

## See also

- `docs/runbooks/remove-node.md` — Remove-a-node runbook
- `cmd/cluster-join/main.go` — Join implementation
- `cmd/cluster-label/main.go` — Label application
- `cmd/placer/main.go` — Model placement policy
