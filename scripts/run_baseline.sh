#!/usr/bin/env bash
set -euo pipefail

INPUT_PDF=${1:-"__fixtures__/fixture.pdf"}
OUT_DIR=${2:-"benchmarks/baseline-tesseract-ocrmypdf"}
PROFILE=${3:-"fast"}

if [[ -x ".venv/bin/ocrpoc" ]]; then
  OCRPOC_BIN=".venv/bin/ocrpoc"
else
  OCRPOC_BIN="ocrpoc"
fi

"$OCRPOC_BIN" run "$INPUT_PDF" --engine tesseract --profile "$PROFILE" --out "$OUT_DIR"
