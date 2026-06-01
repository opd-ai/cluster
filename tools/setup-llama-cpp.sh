#!/usr/bin/env bash
# tools/setup-llama-cpp.sh — Ensure llama.cpp tooling is available.
#
# This script:
#   1. Clones or updates llama.cpp into tools/llama.cpp.
#   2. Builds the convert_lora_to_gguf.py requirements.
#   3. Prints the path to convert_lora_to_gguf.py for use by the pipeline.
#
# The script is idempotent — safe to run multiple times.
#
# Usage:
#   bash tools/setup-llama-cpp.sh
#
# Outputs (stdout, last line):
#   CONVERT_SCRIPT=<absolute path to convert_lora_to_gguf.py>

set -euo pipefail

LLAMA_CPP_DIR="$(cd "$(dirname "$0")" && pwd)/llama.cpp"
LLAMA_CPP_REPO="https://github.com/ggml-org/llama.cpp.git"

# ---- Clone or update llama.cpp -----------------------------------------

if [ -d "$LLAMA_CPP_DIR/.git" ]; then
  echo "[setup-llama-cpp] Updating llama.cpp in $LLAMA_CPP_DIR" >&2
  git -C "$LLAMA_CPP_DIR" fetch --depth=1 origin main
  git -C "$LLAMA_CPP_DIR" reset --hard origin/main
else
  echo "[setup-llama-cpp] Cloning llama.cpp into $LLAMA_CPP_DIR" >&2
  git clone --depth=1 "$LLAMA_CPP_REPO" "$LLAMA_CPP_DIR"
fi

# ---- Install Python requirements ---------------------------------------

CONVERT_SCRIPT="$LLAMA_CPP_DIR/convert_lora_to_gguf.py"

if [ ! -f "$CONVERT_SCRIPT" ]; then
  echo "[setup-llama-cpp] ERROR: $CONVERT_SCRIPT not found after clone." >&2
  exit 1
fi

REQ_FILE="$LLAMA_CPP_DIR/requirements/requirements-convert.txt"
if [ -f "$REQ_FILE" ]; then
  echo "[setup-llama-cpp] Installing Python requirements from $REQ_FILE" >&2
  uv pip install -r "$REQ_FILE" 2>&1 | tail -5 >&2 || true
fi

# ---- Report -------------------------------------------------------------

echo "CONVERT_SCRIPT=$CONVERT_SCRIPT"
