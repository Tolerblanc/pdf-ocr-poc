#!/usr/bin/env bash
set -euo pipefail

INPUT_PDF=${1:-"__fixtures__/fixture.pdf"}
OUT_DIR=${2:-"benchmarks/candidate-apple-vision-fast-v1"}
PROFILE=${3:-"fast"}
MAX_WORKERS=${4:-""}

if [[ -x ".venv/bin/ocrpoc" ]]; then
  OCRPOC_BIN=".venv/bin/ocrpoc"
else
  OCRPOC_BIN="ocrpoc"
fi

EXTRA_ARGS=()
if [[ -n "$MAX_WORKERS" ]]; then
  EXTRA_ARGS+=(--max-workers "$MAX_WORKERS")
fi

caffeinate -dimsu "$OCRPOC_BIN" run "$INPUT_PDF" --engine apple-vision --profile "$PROFILE" "${EXTRA_ARGS[@]}" --out "$OUT_DIR"
