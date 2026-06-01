#!/usr/bin/env python3
"""
train_image_lora.py  —  Image LoRA fine-tuning driver for SDXL and Flux models.

Uses kohya_ss / sd-scripts to train a LoRA adapter from a user-provided
dataset directory.  Outputs a .safetensors file that can be dropped into the
gateway's LoRA library directory.

Exit codes:
  0 — success, LoRA file written to --output-dir
  1 — unrecoverable error
  2 — skipped (dataset too small, or model explicitly excluded)

Usage:
  python train_image_lora.py \
      --dataset-dir /var/lib/aicluster/image_datasets/my_concept \
      --output-dir  /var/lib/aicluster/loras/image \
      --base-model  sdxl \
      --concept     my_concept \
      --steps       1000
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

MIN_IMAGES = 10

BASE_MODEL_CHECKPOINTS: dict[str, str] = {
    "sdxl": "stabilityai/stable-diffusion-xl-base-1.0",
    "flux-dev": "black-forest-labs/FLUX.1-dev",
    "flux-schnell": "black-forest-labs/FLUX.1-schnell",
}

SKIP_MODELS: set[str] = set(
    os.environ.get("SKIP_IMAGE_LORA_MODELS", "").split(",")
) - {""}

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s  %(levelname)-8s  %(message)s",
)
log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Dataset helpers
# ---------------------------------------------------------------------------

def count_images(dataset_dir: Path) -> int:
    """Return the number of image files in *dataset_dir* (non-recursive)."""
    extensions = {".jpg", ".jpeg", ".png", ".webp", ".bmp"}
    return sum(
        1 for p in dataset_dir.iterdir()
        if p.is_file() and p.suffix.lower() in extensions
    )


def write_dataset_config(
    dataset_dir: Path,
    concept: str,
    resolution: int,
    tmp_dir: Path,
) -> Path:
    """Write a kohya_ss dataset config JSON and return its path."""
    config = {
        "datasets": [
            {
                "subsets": [
                    {
                        "image_dir": str(dataset_dir),
                        "caption_extension": ".txt",
                        "class_tokens": concept,
                        "num_repeats": 10,
                    }
                ],
                "resolution": resolution,
                "batch_size": 1,
                "enable_bucket": True,
                "min_bucket_reso": 256,
                "max_bucket_reso": resolution,
            }
        ]
    }
    config_path = tmp_dir / "dataset_config.json"
    config_path.write_text(json.dumps(config, indent=2))
    return config_path


# ---------------------------------------------------------------------------
# Training
# ---------------------------------------------------------------------------

def find_sdscripts() -> Path:
    """Return the sd-scripts directory, raising RuntimeError if absent."""
    candidates = [
        Path(os.environ.get("SD_SCRIPTS_DIR", "")),
        Path.home() / "sd-scripts",
        Path("/opt/sd-scripts"),
    ]
    for c in candidates:
        if c.is_dir() and (c / "train_network.py").exists():
            return c
    raise RuntimeError(
        "sd-scripts not found.  Clone https://github.com/kohya-ss/sd-scripts "
        "and set SD_SCRIPTS_DIR, or place it at ~/sd-scripts."
    )


def train(
    dataset_dir: Path,
    output_dir: Path,
    base_model: str,
    concept: str,
    steps: int,
    resolution: int,
    rank: int,
    learning_rate: float,
) -> Path:
    """Run kohya_ss train_network.py and return the output .safetensors path."""
    sd_scripts = find_sdscripts()
    checkpoint = BASE_MODEL_CHECKPOINTS[base_model]
    output_dir.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp)
        dataset_cfg = write_dataset_config(dataset_dir, concept, resolution, tmp_path)

        output_name = f"{concept}-{base_model}-lora"
        cmd = [
            sys.executable,
            str(sd_scripts / "train_network.py"),
            "--pretrained_model_name_or_path", checkpoint,
            "--dataset_config", str(dataset_cfg),
            "--output_dir", str(output_dir),
            "--output_name", output_name,
            "--network_module", "networks.lora",
            "--network_dim", str(rank),
            "--network_alpha", str(rank // 2),
            "--learning_rate", str(learning_rate),
            "--unet_lr", str(learning_rate),
            "--text_encoder_lr", str(learning_rate / 10),
            "--max_train_steps", str(steps),
            "--optimizer_type", "AdamW8bit",
            "--mixed_precision", "bf16",
            "--gradient_checkpointing",
            "--xformers",
            "--save_model_as", "safetensors",
            "--save_every_n_steps", str(max(steps // 5, 100)),
            "--logging_dir", str(tmp_path / "logs"),
        ]

        log.info("Running: %s", " ".join(cmd))
        result = subprocess.run(cmd, check=False)
        if result.returncode != 0:
            raise RuntimeError(
                f"train_network.py exited with code {result.returncode}"
            )

    output_file = output_dir / f"{output_name}.safetensors"
    if not output_file.exists():
        raise RuntimeError(f"Expected output file not found: {output_file}")
    return output_file


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Image LoRA training driver")
    p.add_argument("--dataset-dir", required=True, type=Path)
    p.add_argument("--output-dir", required=True, type=Path)
    p.add_argument(
        "--base-model",
        default="sdxl",
        choices=list(BASE_MODEL_CHECKPOINTS),
    )
    p.add_argument("--concept", required=True,
                   help="Trigger word / class token for the LoRA")
    p.add_argument("--steps", type=int, default=1000)
    p.add_argument("--resolution", type=int, default=1024)
    p.add_argument("--rank", type=int, default=32)
    p.add_argument("--learning-rate", type=float, default=1e-4)
    p.add_argument("--min-images", type=int, default=MIN_IMAGES)
    return p.parse_args()


def main() -> None:
    args = parse_args()

    if args.base_model in SKIP_MODELS:
        log.info("Skipping %s (in SKIP_IMAGE_LORA_MODELS)", args.base_model)
        sys.exit(2)

    n_images = count_images(args.dataset_dir)
    if n_images < args.min_images:
        log.info(
            "Skipping: only %d image(s) in %s (min %d)",
            n_images, args.dataset_dir, args.min_images,
        )
        sys.exit(2)

    log.info(
        "Training image LoRA: concept=%s base=%s images=%d steps=%d",
        args.concept, args.base_model, n_images, args.steps,
    )

    try:
        output_file = train(
            dataset_dir=args.dataset_dir,
            output_dir=args.output_dir,
            base_model=args.base_model,
            concept=args.concept,
            steps=args.steps,
            resolution=args.resolution,
            rank=args.rank,
            learning_rate=args.learning_rate,
        )
        log.info("LoRA saved to %s", output_file)
    except Exception as exc:
        log.error("Training failed: %s", exc)
        sys.exit(1)


if __name__ == "__main__":
    main()
