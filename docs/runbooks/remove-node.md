# Runbook: Remove a Node

This runbook describes how to safely remove a node from a running cluster
using `cmd/drain`.

---

## Prerequisites

- The cluster is reachable via `cluster/kubeconfig`.
- You have SSH access to the target node.
- All stateful data on the node (adapters, vector shards) has a replica on
  at least one peer node, **or** you accept the data loss.

---

## Steps

### 1. Verify the node is healthy enough to drain

```bash
kubectl get node <hostname>
make status
```

If the node is already `NotReady`, skip to step 4 (delete).

---

### 2. Run `cmd/drain`

```bash
go run ./cmd/drain <hostname>
# or after make build:
bin/drain <hostname>
```

`drain` will, in order:

1. **Cordon** — mark the node unschedulable.
2. **Drain pods** — evict all non-DaemonSet pods (honours PodDisruptionBudgets).
3. **Deregister from gateway** — gateway stops routing inference traffic to this node.
4. **Leave tailnet** — `tailscale logout` on the node via SSH.
5. **Delete k3s node** — `kubectl delete node <hostname>`.

All steps are printed to stdout.  If any step fails the node is left cordoned
and the error is reported; investigate and re-run or proceed manually.

---

### 3. Verify removal

```bash
kubectl get nodes
make status
# Expected: "✓ No drift detected."
```

The inventory still lists the node.  If the removal is permanent, remove it
from `cluster/inventory.yaml` and commit.

---

### 4. Physical decommission (optional)

For permanent removal:

1. Remove the entry from `cluster/inventory.yaml`.
2. Remove the node's SSH authorized key from peer nodes if it was provisioned
   separately.
3. If the node held MinIO data, trigger a MinIO heal:
   ```bash
   mc admin heal -r local/models
   ```
4. If it was a Qdrant replica, check shard health:
   ```bash
   curl http://qdrant.cluster/collections
   ```

---

### Rollback / re-join

To re-add the same node after draining:

```bash
go run ./cmd/cluster-join --inventory cluster/inventory.yaml --script /tmp/join-scripts
scp /tmp/join-scripts/<hostname>-join.sh <hostname>:/tmp/<hostname>-join.sh
ssh <hostname> 'sudo sh /tmp/<hostname>-join.sh'
```

See `docs/runbooks/add-node.md` for the full add-a-node procedure.

---

## See also

- `docs/runbooks/add-node.md` — Add-a-node runbook
- `cmd/drain/main.go` — Drain implementation
- `cmd/cluster-join/main.go` — Re-join implementation
