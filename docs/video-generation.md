# Video Generation

This document describes the video generation tier in the AI cluster.

## Overview

Video generation runs on nodes tagged `role=videogen`.  The same
ComfyUI / SwarmUI scheduling plane used for images handles video jobs,
so quotas, outputs, and the gateway API reuse the Phase 6 plumbing.

## Supported Models

All models listed here are open weights with permissive licenses
suitable for self-hosted use.  Exact checkpoint versions are pinned
in the registry (see `configs/workflows/`).

| Model | License | Resolution | Notes |
|-------|---------|-----------|-------|
| **AnimateDiff** (v3 + motion modules) | Apache 2.0 | 512 × 512 | Classic diffusion-based animation |
| **CogVideoX-5B** | Apache 2.0 | 720 × 480, up to 49 frames | DiT architecture, 5 B parameters |
| **Hunyuan Video** | HunyuanVideo Community License | 720p | High-quality; requires ≥ 24 GB VRAM |
| **LTX-Video** (Lightricks) | Apache 2.0 | 768 × 512 | Real-time-capable; smallest footprint |
| **Wan 2.1** (Wan-AI) | Apache 2.0 | 480 p or 720 p | Strong motion coherence |

> **VRAM requirements**: All models require at least 16 GB GPU VRAM.
> Hunyuan Video requires 24 GB.  The placer enforces these constraints
> via the `min_vram_gb` tag on the `videogen` node pool.

## Node Tagging

```bash
kubectl label node <node> role=videogen
kubectl annotate node <node> min_vram_gb=16
```

Nodes with `min_vram_gb=24` are reserved for Hunyuan Video and other
large models.

## ComfyUI Workflow Files

Pre-built ComfyUI API workflow JSON files live in `configs/workflows/`:

| File | Model |
|------|-------|
| `animatediff-txt2vid.json` | AnimateDiff + SDXL |
| `cogvideox-txt2vid.json` | CogVideoX-5B |
| `hunyuan-txt2vid.json` | Hunyuan Video |
| `ltx-txt2vid.json` | LTX-Video |
| `wan-txt2vid.json` | Wan 2.1 (text→video) |
| `wan-img2vid.json` | Wan 2.1 (image→video) |

## Gateway API

The gateway exposes `/v1/videos/generations` and `/v1/videos/edits`
(see [image & video generation docs](./image-video-generation.md)).  Both endpoints accept
an OpenAI-compatible JSON body and return a `job_id` for polling.

Long-running jobs are tracked in-memory (restart-safe persistence is
on the roadmap).  Outputs land in `outputs/<date>/<job-id>/video.mp4`
plus a preview GIF in the same directory.

## Quota

Video jobs are governed by the same per-key daily quota system as
images (see `--max-videos-per-key-per-day` gateway flag).  Default is
unlimited for self-hosted deployments.

## Scaling

- Add more `role=videogen` nodes to scale throughput horizontally.
- SwarmUI manages the per-backend queue depth and exposes it via
  `/API/ListBackends`, which the gateway probes every N seconds.
- The `/status` gateway endpoint reports `swarm_healthy` for
  observability.
