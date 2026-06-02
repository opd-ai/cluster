#!/bin/sh
# provision-worker.sh — bootstraps the worker node
set -eu

# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh
systemctl enable --now ollama
ollama pull qwen2.5:1.5b

# Wait for control node's k3s to be ready, then join
sleep 30
K3S_TOKEN=$(ssh -o StrictHostKeyChecking=no vagrant@"${CONTROL_IP}" \
  "cat /var/lib/rancher/k3s/server/token")
curl -sfL https://get.k3s.io | K3S_URL="https://${CONTROL_IP}:6443" \
  K3S_TOKEN="${K3S_TOKEN}" sh -s - agent
