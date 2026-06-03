# Image/Video Generation — Deployment Guide

This phase covers the image and video generation tier of the AI cluster,
built on **SwarmUI** + **ComfyUI**.

## Architecture

```
client → gateway /v1/images/generations
              ↓
        SwarmUI (images.cluster:7801)
              ↓
    ┌─────────────────────────────┐
    │  ComfyUI node 1 (CUDA/ROCm) │
    │  ComfyUI node 2 (CUDA/ROCm) │
    │  ComfyUI node 3 (MPS/Metal) │  ← slow tier
    └─────────────────────────────┘
              ↓
   MinIO outputs/<date>/<request-id>/
```

## Node Requirements

| Tag | CPU | GPU VRAM | Notes |
|-----|-----|----------|-------|
| `role=imagegen` | 8+ cores | ≥ 8 GiB | SDXL/Flux |
| `role=videogen` | 16+ cores | ≥ 16 GiB | AnimateDiff / CogVideoX |

## 6.1 SwarmUI Deployment

SwarmUI is an external service — we don't reimplement it.

```bash
# On the designated imagegen node
git clone https://github.com/mcmonkeyprojects/SwarmUI.git /opt/swarmui
cd /opt/swarmui
bash install-linux.sh

# Start as a systemd service
sudo cp configs/swarmui/swarmui.service /etc/systemd/system/
sudo systemctl enable --now swarmui
```

SwarmUI exposes its API on port **7801** by default.
The gateway proxies `/v1/images/*` to `http://images.cluster:7801`.

## 6.2 ComfyUI Backend(s)

SwarmUI auto-discovers ComfyUI backends via its backend configuration.
Add each node's ComfyUI URL in SwarmUI's settings:

```
http://<node-tailnet-ip>:8188
```

Install ComfyUI on each imagegen node manually. `cluster-bootstrap` handles
base node dependencies (for example Ollama/Tailscale), but does not install
ComfyUI itself:

```bash
cluster-bootstrap --inventory cluster/inventory.yaml
```

## 6.3 Multi-Backend Fan-Out

SwarmUI distributes generations across all registered ComfyUI backends.
Configure backends using Tailscale addresses only — never LAN IPs.

## 6.4 Mac/MPS Backend

Apple Silicon nodes with ≥ 16 GiB unified memory can run ComfyUI via MPS.
Mark them as slow-tier in SwarmUI so CUDA nodes are preferred:

```json
{ "TrustLevel": "Slow" }
```

## 6.5 Checkpoint Management

The `cmd/cluster-bootstrap` binary seeds model checkpoints from MinIO on
each image-gen node at bootstrap time.  Required models (by bucket path):

| Model | MinIO Path |
|-------|-----------|
| SDXL Base 1.0 | `checkpoints/sdxl/sd_xl_base_1.0.safetensors` |
| SDXL Refiner | `checkpoints/sdxl/sd_xl_refiner_1.0.safetensors` |
| Flux.1-dev | `checkpoints/flux/flux1-dev.safetensors` |
| Flux.1-schnell | `checkpoints/flux/flux1-schnell.safetensors` |
| CLIP-L | `checkpoints/clip/clip_l.safetensors` |
| T5-XXL | `checkpoints/clip/t5xxl_fp16.safetensors` |
| SDXL VAE | `checkpoints/vae/sdxl_vae.safetensors` |
| FLUX VAE | `checkpoints/vae/ae.safetensors` |

Local layout: `/var/lib/aicluster/hot/models/checkpoints/<model>`.
Symlinks avoid duplicating large files across ComfyUI instances on the same host.

## 6.6 LoRA & Embedding Library

Standard layout on each node:

```
/var/lib/aicluster/hot/models/
  loras/
    sdxl/     ← SDXL LoRA .safetensors
    flux/     ← Flux LoRA .safetensors
  embeddings/ ← textual inversion embeddings
```

The model registry (`cmd/registry`) tracks provenance, license tag, and
SHA256 for every file.  `registry list` prints all tracked files; the `TYPE`
column distinguishes LoRAs from checkpoints and other entries.
