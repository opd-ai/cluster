# Two-Node Quickstart

Bring up a complete two-node AI cluster using **zero-configuration deployment**.
Nodes automatically discover each other via UDP multicast beacons—no manual
inventory editing required.

The cluster runs:

- **Gateway** — OpenAI-compatible API router (auto-discovers backends)
- **Ollama** — LLM inference on both nodes (qwen2.5:1.5b, ~2 GB)
- **RAG** — retrieval-augmented generation service
- **Qdrant** — vector store
- **Console** — Ebitengine WASM web console

**Expected time**: < 30 minutes with zero-conf (faster than manual inventory setup).

## Prerequisites

```bash
# macOS
brew install vagrant virtualbox

# Linux (Debian/Ubuntu)
sudo apt-get install vagrant virtualbox
```

Minimum host resources: 8 CPU cores, 16 GB RAM free, 40 GB disk.

## Start the cluster (Zero-Conf)

```bash
cd examples/quickstart-2node
vagrant up   # ~15 minutes: downloads Ubuntu box, installs dependencies
```

The Vagrant provisioner will:
1. Install Ollama on both nodes.
2. Write `cluster/inventory.yaml` for the two VMs.
3. Bootstrap k3s on the control VM and join the worker VM.

## Verify the cluster

```bash
vagrant ssh control

# Check both nodes are in k3s
kubectl get nodes -o wide

# Check gateway service is running in-cluster
kubectl -n ai-cluster get svc gateway
```

## Query the gateway (LLM inference)

```bash
# From the control node
GATEWAY_KEY=$(kubectl -n ai-cluster get secret gateway-api-keys \
  -o jsonpath='{.data.keys}' | base64 -d | head -1)

curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: ******" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen2.5:1.5b",
    "messages": [{"role": "user", "content": "Hello! Who are you?"}]
  }' | jq '.choices[0].message.content'
```

## Ingest a RAG corpus and query it

```bash
# Ingest the cluster README into a collection
curl -X POST http://localhost:8081/ingest \
  -H "Content-Type: application/json" \
  -d '{"source": "git",
       "url": "https://github.com/opd-ai/cluster",
       "collection": "cluster-docs"}'

# Query via RAG
curl -s http://localhost:8081/query \
  -H "Content-Type: application/json" \
  -d '{"collection": "cluster-docs",
       "query": "How do I add a new node?"}' \
  | jq '.answer'
```

## Generate a sample image (SDXL)

```bash
# SwarmUI must be running; pull a checkpoint first
curl -X POST http://localhost:8080/v1/images/generations \
  -H "Authorization: ******" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "a photo of a mountain at sunset, photorealistic",
    "model": "sdxl-base",
    "size": "1024x1024",
    "n": 1
  }' | jq '.data[0].url'
```

## Open the Ebitengine console

```bash
# On the control node
kubectl port-forward -n ai-cluster svc/console 8082:8080 &
# Then open http://localhost:8082 in a browser (requires WASM support)
```

## Tear down

```bash
vagrant destroy -f
```
