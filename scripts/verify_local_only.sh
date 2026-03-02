#!/usr/bin/env bash
set -euo pipefail

if [[ -x ".venv/bin/python" ]]; then
  PYTHON_BIN=".venv/bin/python"
else
  PYTHON_BIN="python3"
fi

RUN_DIR=${1:-""}

if [[ -n "$RUN_DIR" ]]; then
  "$PYTHON_BIN" - "$RUN_DIR" <<'PY'
import json
import sys
from pathlib import Path

run_dir = Path(sys.argv[1])
report_path = run_dir / "local_only_report.json"
if not report_path.exists():
    raise SystemExit(f"missing local-only report: {report_path}")

data = json.loads(report_path.read_text(encoding="utf-8"))
if not data.get("monitor_ok"):
    raise SystemExit("local-only monitor detected remote connection violations")
print("local-only report validated")
PY
else
  "$PYTHON_BIN" -m pdf_ocr_poc.cli selfcheck-local-only
fi
