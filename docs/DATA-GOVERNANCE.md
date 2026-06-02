# Data Governance

> **Scope**: This document defines what data leaves each node in the AI cluster,
> enforces per-namespace egress rules, and provides deletion runbooks for
> administrators managing training/generation/RAG data.

## 1. Data Flows and Boundaries

### 1.1 RAG Retrieval

| Data | Source | Destination | Leaves Node? |
|------|--------|-------------|--------------|
| User query text | Client → Gateway | RAG pod | No (intra-cluster) |
| Embedding request | RAG pod | Ollama (in-cluster) | No |
| Vector similarity results | Qdrant (in-cluster) | RAG pod | No |
| Retrieved document chunks | Qdrant → RAG | RAG → Gateway | No (intra-cluster only) |
| Final answer | RAG → Gateway | Client (external) | Yes — answer only |

**No raw documents or embeddings ever leave the cluster.**
Qdrant is network-policy-restricted to accept connections only from RAG and gateway pods
(see `cluster/overlays/production/network-policies.yaml`).

### 1.2 Image / Video Generation

| Data | Source | Destination | Leaves Node? |
|------|--------|-------------|--------------|
| Text prompt | Client → Gateway | SwarmUI / ComfyUI (in-cluster) | No |
| Generated image/video | SwarmUI / ComfyUI | Gateway → Client | Yes — output only |
| Model weights | MinIO (in-cluster) | SwarmUI / ComfyUI | No |

SwarmUI and ComfyUI pods have no outbound network access except via the gateway
(enforced by NetworkPolicy `swarmui-restricted-ingress` /
`comfyui-restricted-ingress`).

### 1.3 Federated Training

| Data | Source | Destination | Leaves Node? |
|------|--------|-------------|--------------|
| Raw training data | Local dataset | Stays on worker node | **Never** |
| Gradient updates | Worker node | Federation aggregator (port 8443) | Yes — gradients only |
| Aggregated model | Aggregator | Worker nodes | Yes — weights only |

Raw training data never leaves the worker node. The `training-gradient-egress`
NetworkPolicy (`cluster/overlays/production/network-policies.yaml`) permits
egress only on port 8443 (the aggregator endpoint) for pods labelled
`role: federated-worker`.

### 1.4 System Telemetry

| Data | Source | Destination | Leaves Cluster? |
|------|--------|-------------|-----------------|
| Prometheus metrics | All pods | Prometheus (in-cluster) | No |
| Loki logs | All pods | Loki (in-cluster) | No |
| OpenTelemetry traces | Gateway, RAG | Tempo (in-cluster) | No |
| Alertmanager notifications | Alertmanager | Operator-configured webhook (external) | Yes — alert text only |

Alertmanager webhooks should be limited to PagerDuty, Slack, or similar services
that the operator controls. No raw log data or traces are forwarded externally.

---

## 2. Per-Namespace Egress Rules

| Namespace | Allowed Egress | Blocked Egress |
|-----------|---------------|----------------|
| `ai-cluster` | DNS (53/udp+tcp), Ollama (11434), internal services | Internet, except via explicit policy |
| `monitoring` | DNS, ai-cluster scrape ports (9100, 9401, 8080, 8081, 6333, 11434) | Internet |
| `training` | DNS, aggregator (8443/tcp) | All other outbound |
| `kube-system` | Cluster-internal (no restriction) | — |

See `cluster/overlays/production/network-policies.yaml` for the full set of
`NetworkPolicy` resources enforcing these rules.

---

## 3. Deletion Runbooks

### 3.1 "Forget this repository" — remove a RAG collection

Removes all documents ingested from a specific Git repository from the Qdrant
vector store.

```bash
# Identify the collection name (typically the repo slug or a custom name
# assigned at ingest time)
COLLECTION="my-repo-name"

# Delete the collection via Qdrant REST API (from within the cluster)
kubectl -n ai-cluster exec -it deployment/rag -- \
  curl -s -X DELETE http://qdrant:6333/collections/${COLLECTION}

# Verify deletion
kubectl -n ai-cluster exec -it deployment/rag -- \
  curl -s http://qdrant:6333/collections | jq '.result.collections[].name'
```

If the collection was ingested via the RAG service's `/ingest` endpoint, also
delete the source reference from the RAG service's metadata store (if
configured).

### 3.2 "Purge generated outputs older than N days"

Generated images and videos are stored in MinIO under the `outputs` bucket.

```bash
# Install the MinIO client inside the cluster
kubectl -n ai-cluster run minio-cleanup --rm -it --restart=Never \
  --image=minio/mc -- sh -c '
  mc alias set cluster http://minio:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD
  # Delete objects older than N days in the outputs bucket
  mc find cluster/outputs --older-than ${N}d --exec "mc rm {}"
  '
```

Replace `N` with the retention period in days (e.g., `30`).

To automate this, create a CronJob:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: outputs-gc
  namespace: ai-cluster
spec:
  schedule: "0 3 * * *"   # daily at 03:00
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: gc
              image: minio/mc
              env:
                - name: MINIO_ROOT_USER
                  valueFrom:
                    secretKeyRef:
                      name: minio-root-credentials
                      key: root-user
                - name: MINIO_ROOT_PASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: minio-root-credentials
                      key: root-password
                - name: RETENTION_DAYS
                  value: "30"
              command:
                - sh
                - -c
                - |
                  mc alias set cluster http://minio:9000 \
                    $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD
                  mc find cluster/outputs \
                    --older-than ${RETENTION_DAYS}d \
                    --exec "mc rm {}"
```

### 3.3 "Drop RAG collection X"

Same as 3.1 but explicitly targets a named collection:

```bash
COLLECTION="my-collection"
kubectl -n ai-cluster exec -it deployment/rag -- \
  curl -s -X DELETE http://qdrant:6333/collections/${COLLECTION}
```

To re-ingest after deletion:

```bash
# POST to the RAG ingest endpoint (replace URL and payload)
kubectl -n ai-cluster port-forward svc/rag 8081:8081 &
curl -X POST http://localhost:8081/ingest \
  -H "Content-Type: application/json" \
  -d '{"source": "git", "url": "https://github.com/org/repo", "collection": "my-collection"}'
```

### 3.4 Purge Prometheus / Loki metrics and logs

**Prometheus** (delete series matching a label selector):

```bash
# Identify the Prometheus pod
POD=$(kubectl -n monitoring get pod -l app=prometheus -o name | head -1)

# Delete all series for a specific job (e.g. old exporter)
kubectl -n monitoring exec "$POD" -- \
  curl -s -X POST \
  'http://localhost:9090/api/v1/admin/tsdb/delete_series?match[]=job="old-exporter"'

# Run a clean tombstone compaction
kubectl -n monitoring exec "$POD" -- \
  curl -s -X POST 'http://localhost:9090/api/v1/admin/tsdb/clean_tombstones'
```

**Loki** (delete log streams matching a label set):

```bash
START=$(date -u --date='7 days ago' +%s%N)   # epoch nanoseconds
END=$(date -u +%s%N)

kubectl -n monitoring port-forward svc/loki 3100:3100 &
curl -s -g -X POST \
  "http://localhost:3100/loki/api/v1/delete?query={namespace=\"ai-cluster\",app=\"rag\"}&start=${START}&end=${END}"
```

> **Note**: Loki delete API requires `--store.retention-delete-worker-count > 0`
> (set in `cluster/overlays/production/logging.yaml`).

---

## 4. Data Classification

| Classification | Examples | Retention | Delete on request |
|----------------|----------|-----------|------------------|
| User queries | Prompt text in logs | 30 days (Loki TTL) | Yes — via Loki delete API |
| Generated outputs | Images, videos in MinIO | 30 days (CronJob GC) | Yes — via `mc rm` |
| Embeddings | Qdrant vectors | Lifetime of collection | Yes — via collection delete |
| Model weights | Ollama models, MinIO | Until explicit removal | Yes — `ollama rm` / `mc rm` |
| Gradient updates | In-flight only | Not persisted | N/A |
| System metrics | Prometheus TSDB | 15 days (Prometheus retention) | Yes — via admin API |
| System logs | Loki | 30 days (retention period) | Yes — via Loki delete API |
| Traces | Tempo | 7 days (default Tempo retention) | Yes — via Tempo delete API |

---

## 5. Compliance Notes

- All data processing occurs within the operator-controlled cluster nodes.
- No user data is sent to external services by default.
- Alertmanager webhooks are the only configured external data channel; they
  carry alert *metadata* only (metric names, labels, threshold values) — not
  user content or model outputs.
- For GDPR Article 17 (right to erasure): use the runbooks in Section 3.
- For federated training privacy budgets, see `docs/FEDERATED-PRIVACY.md`.
