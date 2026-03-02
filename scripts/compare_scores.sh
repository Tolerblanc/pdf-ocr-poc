#!/usr/bin/env bash
set -euo pipefail

BASE_DIR=${1:-"benchmarks/baseline-tesseract-ocrmypdf"}
CAND_DIR=${2:-"benchmarks/candidate-paddle"}
GOLD=${3:-"fixtures/gold/v1/gold-pages.json"}
OUT=${4:-"benchmarks/comparison.json"}

if [[ -x ".venv/bin/ocrpoc" ]]; then
  OCRPOC_BIN=".venv/bin/ocrpoc"
else
  OCRPOC_BIN="ocrpoc"
fi

if [[ -x ".venv/bin/python" ]]; then
  PYTHON_BIN=".venv/bin/python"
else
  PYTHON_BIN="python3"
fi

"$PYTHON_BIN" scripts/eval_metrics.py --gold "$GOLD" --pred "$BASE_DIR/pages.json" --out "$BASE_DIR/eval.json"
"$PYTHON_BIN" scripts/eval_metrics.py --gold "$GOLD" --pred "$CAND_DIR/pages.json" --out "$CAND_DIR/eval.json"

"$OCRPOC_BIN" compare \
  --baseline-eval "$BASE_DIR/eval.json" \
  --candidate-eval "$CAND_DIR/eval.json" \
  --baseline-report "$BASE_DIR/run_report.json" \
  --candidate-report "$CAND_DIR/run_report.json" \
  --out "$OUT"
