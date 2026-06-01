#!/usr/bin/env bash
# ci/check-wasm-size.sh — enforce the Phase 8.15 WASM size budget.
#
# Usage: ./ci/check-wasm-size.sh [path-to-main.wasm]
#
# The script:
#   1. Compiles cmd/console-wasm to main.wasm (or uses a pre-built binary).
#   2. Gzip-compresses the binary and checks the compressed size.
#   3. Exits 0 if the gzipped size is ≤ MAX_GZIP_MB; exits 1 otherwise.
#
# Thresholds (per Phase 8.15):
#   - Gzipped WASM ≤ 15 MB
#
# Designed to run in GitHub Actions or any POSIX CI environment.
set -euo pipefail

readonly MAX_GZIP_BYTES=$((15 * 1024 * 1024))  # 15 MB

WASM_PATH="${1:-}"
TMPDIR_LOCAL=$(mktemp -d)
trap 'rm -rf "$TMPDIR_LOCAL"' EXIT

# ── Build if no pre-built binary is provided ─────────────────────────────────
if [[ -z "$WASM_PATH" ]]; then
  echo "Building cmd/console-wasm…"
  WASM_PATH="$TMPDIR_LOCAL/main.wasm"
  GOOS=js GOARCH=wasm go build -o "$WASM_PATH" ./cmd/console-wasm/...
fi

if [[ ! -f "$WASM_PATH" ]]; then
  echo "ERROR: WASM binary not found at $WASM_PATH" >&2
  exit 1
fi

RAW_SIZE=$(wc -c < "$WASM_PATH")
GZIP_PATH="$TMPDIR_LOCAL/main.wasm.gz"
gzip -9 --keep -c "$WASM_PATH" > "$GZIP_PATH"
GZIP_SIZE=$(wc -c < "$GZIP_PATH")

raw_mb=$(awk "BEGIN {printf \"%.2f\", $RAW_SIZE / 1048576}")
gz_mb=$(awk "BEGIN {printf \"%.2f\", $GZIP_SIZE / 1048576}")

echo "WASM size:        ${raw_mb} MB (raw)"
echo "WASM size:        ${gz_mb} MB (gzipped)"
echo "Budget:           $((MAX_GZIP_BYTES / 1048576)) MB gzipped"

if [[ "$GZIP_SIZE" -gt "$MAX_GZIP_BYTES" ]]; then
  echo "FAIL: gzipped WASM ${gz_mb} MB exceeds budget of 15 MB" >&2
  exit 1
fi

echo "PASS: gzipped WASM is within the 15 MB budget."
