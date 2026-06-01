"""
train.py — LoRA fine-tuning driver for the AI cluster pipeline.

Modes:
  --mode namespace   Train a namespace-wide LoRA on the merged dataset.
  --mode repo        Train a per-repo LoRA atop the namespace base model.

Both modes read configs/namespaces.yaml for hyperparameters, write
merged 16-bit weights and a GGUF to the output directory, then exit.

The Go pipeline (cmd/pipeline) invokes this script via os/exec with
explicit --mode, --namespace, and --repo arguments so it remains
fully controllable from the Go side.

Usage:
  python train.py \\
      --mode namespace \\
      --namespace core \\
      --namespaces ../configs/namespaces.yaml \\
      --dataset-dir ../datasets/core/dataset.jsonl \\
      --output-dir ../checkpoints/core/namespace

  python train.py \\
      --mode repo \\
      --namespace core \\
      --repo cluster \\
      --namespaces ../configs/namespaces.yaml \\
      --dataset-dir ../datasets/core/repos/cluster/dataset.jsonl \\
      --base-model ../checkpoints/core/namespace/merged \\
      --output-dir ../checkpoints/core/repos/cluster

Exit codes:
  0  Success.
  1  Fatal error (dataset not found, OOM, etc.).
  2  Skipped (repo below repo_min_samples threshold).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import yaml


def load_namespaces(path: str) -> dict:
    with open(path) as f:
        return yaml.safe_load(f)


def get_ns_config(cfg: dict, ns_name: str) -> dict | None:
    for ns in cfg.get("namespaces", []):
        if ns["name"] == ns_name:
            return ns
    return None


def merge_hyperparams(global_hp: dict, ns_hp: dict) -> dict:
    merged = dict(global_hp)
    for k, v in ns_hp.items():
        if v:
            merged[k] = v
    return merged


def count_examples(dataset_path: str) -> int:
    p = Path(dataset_path)
    if not p.exists():
        return 0
    return sum(1 for line in p.open() if line.strip())


def run_training(args: argparse.Namespace, hp: dict, base_model: str) -> None:
    """Perform the actual fine-tuning using Unsloth + TRL."""
    try:
        from unsloth import FastLanguageModel  # type: ignore[import]
        from trl import SFTTrainer, SFTConfig  # type: ignore[import]
        from datasets import load_dataset  # type: ignore[import]
        import torch  # type: ignore[import]
    except ImportError as e:
        print(f"[train] Missing dependency: {e}", file=sys.stderr)
        print("[train] Install with: uv pip install -e '.[dev]' from python/", file=sys.stderr)
        sys.exit(1)

    max_seq = hp.get("max_seq_length", 4096)
    dtype = torch.bfloat16 if torch.cuda.is_bf16_supported() else torch.float16

    print(f"[train] Loading base model: {base_model}")
    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=base_model,
        max_seq_length=max_seq,
        dtype=dtype,
        load_in_4bit=True,
    )
    model = FastLanguageModel.get_peft_model(
        model,
        r=hp.get("lora_rank", 8),
        lora_alpha=hp.get("lora_alpha", 32),
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj",
                        "gate_proj", "up_proj", "down_proj"],
        lora_dropout=0.0,
        bias="none",
        use_gradient_checkpointing="unsloth",
    )

    dataset = load_dataset("json", data_files=args.dataset_dir, split="train")
    print(f"[train] Dataset size: {len(dataset)} examples")

    out = Path(args.output_dir)
    out.mkdir(parents=True, exist_ok=True)

    training_args = SFTConfig(
        output_dir=str(out / "checkpoints"),
        num_train_epochs=1,
        max_steps=hp.get("max_steps", 300),
        per_device_train_batch_size=hp.get("batch_size", 2),
        gradient_accumulation_steps=hp.get("grad_accum", 4),
        learning_rate=hp.get("learning_rate", 2e-4),
        max_seq_length=max_seq,
        save_steps=100,
        logging_steps=10,
        fp16=dtype == torch.float16,
        bf16=dtype == torch.bfloat16,
        report_to="none",
    )
    trainer = SFTTrainer(
        model=model,
        tokenizer=tokenizer,
        train_dataset=dataset,
        dataset_text_field="text",
        args=training_args,
    )
    trainer.train()

    # Save merged 16-bit weights.
    merged_dir = str(out / "merged")
    print(f"[train] Saving merged 16-bit model to {merged_dir}")
    model.save_pretrained_merged(merged_dir, tokenizer, save_method="merged_16bit")

    # Save GGUF.
    gguf_path = str(out / "model.gguf")
    quant = hp.get("quantization", "q4_k_m")
    print(f"[train] Saving GGUF ({quant}) to {gguf_path}")
    model.save_pretrained_gguf(str(out / "model"), tokenizer, quantization_method=quant)

    # Write a manifest for the registry.
    manifest = {
        "mode": args.mode,
        "namespace": args.namespace,
        "repo": getattr(args, "repo", None),
        "base_model": base_model,
        "merged_dir": merged_dir,
        "gguf_path": gguf_path,
        "hyperparams": hp,
    }
    with open(out / "manifest.json", "w") as f:
        json.dump(manifest, f, indent=2)
    print(f"[train] Done. Manifest written to {out / 'manifest.json'}")


def main() -> None:
    parser = argparse.ArgumentParser(description="LoRA fine-tuning driver")
    parser.add_argument("--mode", required=True, choices=["namespace", "repo"])
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--repo", default="")
    parser.add_argument("--namespaces", default="../configs/namespaces.yaml")
    parser.add_argument("--dataset-dir", required=True)
    parser.add_argument("--base-model", default="")
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()

    cfg = load_namespaces(args.namespaces)
    ns_cfg = get_ns_config(cfg, args.namespace)
    if ns_cfg is None:
        print(f"[train] Namespace '{args.namespace}' not found in {args.namespaces}", file=sys.stderr)
        sys.exit(1)

    global_hp = cfg.get("global", {}).get("hyperparams", {})
    ns_hp = ns_cfg.get("hyperparams", {})
    hp = merge_hyperparams(global_hp, ns_hp)

    # For repo mode: check min_samples threshold.
    if args.mode == "repo":
        if not args.repo:
            print("[train] --repo is required for mode=repo", file=sys.stderr)
            sys.exit(1)
        skip_repos = ns_cfg.get("skip_repo_lora", [])
        if args.repo in skip_repos:
            print(f"[train] Repo '{args.repo}' is in skip_repo_lora; skipping.")
            sys.exit(2)
        min_samples = ns_cfg.get("repo_min_samples",
                                  cfg.get("global", {}).get("repo_min_samples", 50))
        n = count_examples(args.dataset_dir)
        if n < min_samples:
            print(f"[train] Repo '{args.repo}' has {n} examples < min {min_samples}; skipping.")
            sys.exit(2)

    # Resolve base model.
    base_model = args.base_model or ns_cfg.get("base_model", cfg.get("global", {}).get("base_model", "llama3:8b"))
    run_training(args, hp, base_model)


if __name__ == "__main__":
    main()
