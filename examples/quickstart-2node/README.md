# Two-Node Quickstart

Bring up a complete two-node AI cluster on your laptop using Vagrant and
VirtualBox. The cluster runs:

- **Gateway** — OpenAI-compatible API router
- **Ollama** — LLM inference on both nodes (qwen2.5:1.5b, ~2 GB)
- **RAG** — retrieval-augmented generation service
- **Qdrant** — vector store
- **Console** — Ebitengine WASM web console

**Expected time**: < 60 minutes on a modern laptop (SSD, 16 GB RAM, 8 cores).

## Prerequisites

```bash
# macOS
brew install vagrant virtualbox

# Linux (Debian/Ubuntu)
sudo apt-get install vagrant virtualbox
```

Minimum host resources: 8 CPU cores, 16 GB RAM free, 40 GB disk.

## Start the cluster

```bash
cd examples/quickstart-2node
vagrant up   # ~20 minutes: downloads Ubuntu box, installs k3s, Ollama
```

## Verify the cluster

```bash
vagrant ssh control
kubectl get nodes      # should show control + worker1 Ready
kubectl get pods -A    # gateway, rag, qdrant, console should be Running
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
