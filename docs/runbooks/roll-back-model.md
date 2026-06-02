# Runbook: Roll Back a Model

Use this runbook when a newly deployed model produces regressions in quality,
latency, or stability.

## Prerequisites

- `kubectl` access
- Ollama installed on the affected node(s)
- The previous model version's SHA (from `cluster/inventory.yaml` git history)

---

## Steps

### 1. Identify the previous model SHA

```bash
git log --oneline cluster/inventory.yaml | head -5
# Pick the commit before the bad model was added
git show <commit>:cluster/inventory.yaml | grep -A2 "hostname: <node>"
```

### 2. Pull the previous model version on the affected node

```bash
# SSH into the node
ssh ubuntu@<node-address>

# Pull by SHA (if the previous version had a pinned SHA)
ollama pull <model-name>@sha256:<old-sha>

# Or pull by tag if SHA is unavailable
ollama pull <model-name>:<old-tag>
```

### 3. Update the inventory

Edit `cluster/inventory.yaml` and revert the model SHA/tag to the previous value.

```bash
git diff cluster/inventory.yaml   # verify the revert
git add cluster/inventory.yaml
git commit -m "revert: roll back <model> to <old-version>"
```

### 4. Signal the gateway to reload

```bash
# SIGHUP causes the gateway to re-read the inventory without downtime
kubectl -n ai-cluster exec deployment/gateway -- kill -HUP 1
```

### 5. Verify

```bash
curl http://gateway/v1/models | jq '.data[].id'
# Confirm the old model is listed

curl http://gateway/v1/chat/completions \
  -H "Authorization: ******" \
  -d '{"model": "<model-name>", "messages": [{"role":"user","content":"test"}]}'
# Confirm the response is from the old model
```

### 6. Remove the bad model from all nodes (optional)

```bash
ssh ubuntu@<node-address> ollama rm <bad-model-name>
```
