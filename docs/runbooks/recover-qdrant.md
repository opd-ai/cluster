# Runbook: Recover Qdrant

Use this runbook when Qdrant is unavailable, has corrupted data, or needs
to be migrated to a new node.

## Prerequisites

- `kubectl` access
- Access to the MinIO backup bucket (or a recent Qdrant snapshot)

---

## Scenario A: Pod is crash-looping

```bash
# Describe the pod for the error
kubectl -n ai-cluster describe pod -l app=qdrant

# Common fix: storage volume full — expand the PVC
kubectl -n ai-cluster edit pvc qdrant-data
# Increase spec.resources.requests.storage, then:
kubectl -n ai-cluster rollout restart deployment/qdrant
```

## Scenario B: Data corruption — restore from snapshot

### 1. Identify the latest backup

```bash
kubectl -n ai-cluster exec deployment/backup-agent -- \
  mc ls cold/snapshots | grep qdrant | sort | tail -5
```

### 2. Restore the Qdrant snapshot

```bash
SNAPSHOT_FILE="qdrant-<collection>-<timestamp>.snapshot"

# Copy snapshot into the Qdrant pod
kubectl -n ai-cluster cp "${SNAPSHOT_FILE}" \
  "$(kubectl -n ai-cluster get pod -l app=qdrant -o name | head-1)":/qdrant/snapshots/

# Restore via Qdrant REST API
COLLECTION="my-collection"
kubectl -n ai-cluster exec -it deployment/qdrant -- \
  curl -s -X POST \
  "http://localhost:6333/collections/${COLLECTION}/snapshots/recover" \
  -H "Content-Type: application/json" \
  -d "{\"location\": \"file:///qdrant/snapshots/${SNAPSHOT_FILE}\"}"
```

### 3. Verify

```bash
kubectl -n ai-cluster exec -it deployment/qdrant -- \
  curl -s http://localhost:6333/collections | jq '.result.collections[].name'
```

## Scenario C: Migrate to a new node

```bash
# 1. Take a fresh snapshot of all collections
collections=$(kubectl -n ai-cluster exec deployment/qdrant -- \
  curl -s http://localhost:6333/collections | jq -r '.result.collections[].name')

for col in $collections; do
  kubectl -n ai-cluster exec deployment/qdrant -- \
    curl -s -X POST "http://localhost:6333/collections/${col}/snapshots"
done

# 2. Delete the old PVC node affinity (or create a new PVC on the target node)
kubectl -n ai-cluster delete pvc qdrant-data
# Re-apply kustomization (creates new PVC), then restore via Scenario B.
```
