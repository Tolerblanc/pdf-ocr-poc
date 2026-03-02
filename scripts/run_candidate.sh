#!/usr/bin/env bash
set -euo pipefail

INPUT_PDF=${1:-"__fixtures__/fixture.pdf"}
OUT_DIR=${2:-"benchmarks/candidate-paddle"}
PROFILE=${3:-"quality"}

if [[ -x ".venv/bin/ocrpoc" ]]; then
  OCRPOC_BIN=".venv/bin/ocrpoc"
else
  OCRPOC_BIN="ocrpoc"
fi

"$OCRPOC_BIN" run "$INPUT_PDF" --engine paddle --profile "$PROFILE" --out "$OUT_DIR"
