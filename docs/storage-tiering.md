# Storage Tiering — Phase 3.1

All persistent data in the cluster is organised into three tiers.  Each tier
is optimised for its access pattern and hardware requirements.

## Tier definitions

| Tier   | Mount / endpoint                        | Typical hardware        | Contents |
|--------|-----------------------------------------|-------------------------|----------|
| `hot`  | `/var/lib/aicluster/cache` per node     | Local NVMe              | Active model weights, KV-cache pages, recently generated images/video frames |
| `warm` | `s3://storage.cluster` (MinIO)          | Network-attached HDD/SSD | Datasets, LoRA adapters, image/video checkpoints (SDXL, Flux, video models, VAEs, CLIP/T5), vector indexes, training logs, repo-cache bare clones |
| `cold` | Object store snapshots (MinIO versioning or external S3/B2) | Cheapest available | Snapshots, audit logs, model versions older than N releases |

## Hot tier

- **Path**: `/var/lib/aicluster/cache`  
- **Provisioned by**: `cmd/cluster-bootstrap` (creates the directory and sets
  the mount unit on each node's NVMe partition).  
- **Eviction**: `cmd/cache-gc` runs as a systemd timer; LRU policy with a
  configurable high-water mark (default: 85 % of partition capacity).  
- **Contents**: symlinks from this path into the Ollama model store so that
  the hot tier IS the model store — no copy step needed.

## Warm tier

- **Endpoint**: `http://storage.cluster:9000` (MinIO S3-compatible API)  
- **Provisioned by**: Phase 3.2 bootstrap.  
- **Replication**: two-way sync to a second node (if available) using MinIO's
  built-in server-side replication.  
- **Bucket layout**:

  | Bucket            | Contents |
  |-------------------|----------|
  | `models/`         | GGUF blobs, base weights |
  | `datasets/`       | Training corpora (JSONL) |
  | `adapters/`       | LoRA PEFT adapters, GGUF adapters |
  | `checkpoints/`    | SDXL/Flux/video model files, VAEs, CLIP/T5 encoders, LoRAs |
  | `outputs/`        | Generated images, video clips, audio |
  | `rag/`            | Corpora, Qdrant snapshots, BM25 indexes |
  | `snapshots/`      | etcd + MinIO bucket snapshots |
  | `logs/`           | Training logs, eval reports |

## Cold tier

- Implemented via MinIO versioning (objects are never deleted, only
  transitioned to a cheaper storage class) or an external S3/Backblaze B2
  bucket configured as MinIO's remote tiering target.
- Nightly lifecycle rule moves objects older than 30 days from `snapshots/`
  and `logs/` to the cold target.
- See Phase 12 (Backup & Disaster Recovery) for snapshot and restore
  procedures.
