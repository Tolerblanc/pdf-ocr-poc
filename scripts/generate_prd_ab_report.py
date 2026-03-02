#!/usr/bin/env python3
from __future__ import annotations

import argparse
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
SRC_PATH = REPO_ROOT / "src"
if str(SRC_PATH) not in sys.path:
    sys.path.insert(0, str(SRC_PATH))

from pdf_ocr_poc.reporting import (  # type: ignore[import-not-found]
    generate_prd_ab_report,
    parse_candidate_arg,
)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate PRD-format A/B benchmark report from run artifacts"
    )
    parser.add_argument(
        "--baseline-dir",
        default="benchmarks/baseline-tesseract-ocrmypdf",
        help="Baseline run directory containing run_report.json/eval.json",
    )
    parser.add_argument(
        "--candidate",
        action="append",
        default=[
            "paddle-fast-v3=benchmarks/candidate-paddle-fast-v3",
            "apple-vision-fast-v2=benchmarks/candidate-apple-vision-fast-v2",
        ],
        help="Candidate in name=path form (repeatable)",
    )
    parser.add_argument(
        "--out",
        default="docs/prd-ab-report.md",
        help="Output markdown report path",
    )
    args = parser.parse_args()

    baseline_dir = (REPO_ROOT / args.baseline_dir).resolve()
    candidates = [parse_candidate_arg(item, REPO_ROOT) for item in args.candidate]
    output = generate_prd_ab_report(
        repo_root=REPO_ROOT,
        baseline_dir=baseline_dir,
        candidates=candidates,
        output_path=(REPO_ROOT / args.out).resolve(),
    )

    print(f"Wrote PRD A/B report: {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
