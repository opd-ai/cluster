# Mixed-Fleet Example

This example demonstrates a heterogeneous three-node cluster using **zero-configuration deployment**. Nodes automatically discover each other via UDP multicast beacons.

| Node | Hardware | Workloads |
|------|----------|-----------|
| `gpu-box` | Linux, NVIDIA RTX 4090 | LLM (70B), image gen (SDXL/Flux), video gen |
| `mac-mini` | macOS, Apple M2 Pro | LLM (13B), embeddings |
| `cpu-node` | Linux, CPU-only | RAG ingestion, Qdrant |

## Zero-Configuration Setup (Recommended)

With zero-conf, each node auto-discovers the others. No inventory file needed.

### 1. Deploy on gpu-box (Linux + NVIDIA)

```bash
# On gpu-box
git clone https://github.com/opd-ai/cluster.git && cd cluster
make build

# Deploy services (auto-detects NVIDIA GPU and VRAM)
make deploy ROLES=chat,image-generation

# Start node-agent for discovery
NODE_AGENT_API_KEY=change-me
go run ./cmd/node-agent --roles chat,image-generation --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

### 2. Deploy on mac-mini (macOS + Apple Silicon)

```bash
# On mac-mini
git clone https://github.com/opd-ai/cluster.git && cd cluster
make build

# Deploy services (auto-detects Apple Silicon)
make deploy ROLES=chat

# Start node-agent for discovery
go run ./cmd/node-agent --roles chat --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

### 3. Deploy on cpu-node (Linux, CPU-only)

```bash
# On cpu-node
git clone https://github.com/opd-ai/cluster.git && cd cluster
make build

# Deploy RAG/Qdrant services
make deploy ROLES=rag

# Start node-agent for discovery
go run ./cmd/node-agent --roles rag --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

### 4. Verify discovery

All nodes automatically discover each other. From any node:

```bash
# Check discovered peers
curl -H "Authorization: ******" http://localhost:9977/api/v1/peers | jq

# Check gateway has all backends
curl -H "Authorization: ******" http://gpu-box:8080/v1/models | jq '.data[].id'
```

---

## Manual Inventory (Legacy)

For environments requiring explicit configuration:

```yaml
# cluster/inventory.yaml
nodes:
  - hostname: gpu-box
    address: 100.64.0.10
    ssh_user: ubuntu
    arch: amd64
    os: linux
    role: worker
    hardware: nvidia
    gpu_vram_gb: 24
    models:
      - name: llama3.3:70b
      - name: sdxl-base
      - name: flux-dev

  - hostname: mac-mini
    address: 100.64.0.11
    ssh_user: admin
    arch: arm64
    os: darwin
    role: worker
    unified_memory_gb: 32
    models:
      - name: llama3.2:13b
      - name: nomic-embed-text

  - hostname: cpu-node
    address: 100.64.0.12
    ssh_user: ubuntu
    arch: amd64
    os: linux
    role: worker
    models: []
```

## Routing Behaviour

The gateway routes requests to the appropriate backend (auto-discovered or from inventory):

- `/v1/chat/completions` with `model: llama3.3:70b` → `gpu-box:11434`
- `/v1/chat/completions` with `model: llama3.2:13b` → `mac-mini:11434`
- `/v1/embeddings` → `mac-mini:11434` (nomic-embed-text)
- `/v1/images/generations` → `gpu-box:7801` (SwarmUI/SDXL)
- `/v1/videos/generations` → `gpu-box:7801` (SwarmUI/video model)
- RAG ingest → `cpu-node` (Qdrant via RAG service)

## Bootstrap (Legacy Manual Path)

For manual inventory deployments:

```bash
# From the control node (or any node with SSH access to all three)
make bootstrap   # runs cmd/cluster-bootstrap against all nodes in inventory
make up          # installs k3s on Linux nodes, configures launchd on mac-mini
```

## Deploying the GPU workloads

NVIDIA nodes must be labelled before scheduling GPU workloads:

```bash
kubectl label node gpu-box hardware=nvidia
```

The `dcgm-exporter` DaemonSet (see `cluster/overlays/production/dcgm-exporter.yaml`)
uses `nodeSelector: hardware: nvidia` and will schedule automatically.

## Mac Mini integration

The Mac Mini runs Ollama natively under `launchd` — it does **not** join k3s.
The gateway routes to it via inventory entries (or discovery if enabled with `--discovery=true`).

```bash
# On mac-mini: ensure Ollama is running
brew install ollama
brew services start ollama
ollama pull llama3.2:13b
ollama pull nomic-embed-text
```

Verify the gateway can reach it:

```bash
curl http://100.64.0.11:11434/api/tags | jq '.models[].name'
```

## CPU-only node (RAG ingestion)

The `cpu-node` runs Qdrant and the RAG ingestion service but no LLM. This
frees GPU/ANE resources on the other nodes for inference.

```bash
kubectl -n ai-cluster scale deployment/rag --replicas=1
# Qdrant is already scheduled on cpu-node via nodeSelector: role=storage
```

## Observability

All three nodes are scraped by Prometheus:

- `gpu-box` — dcgm-exporter (GPU), node-exporter (system)
- `mac-mini` — powermetrics-exporter on :9401 (Apple Silicon power)
- `cpu-node` — node-exporter (system)

The `cluster-overview` and `gpu-vram-per-node` Grafana dashboards show all
three nodes side by side.
