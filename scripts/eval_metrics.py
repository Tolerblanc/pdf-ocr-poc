#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
SRC_PATH = REPO_ROOT / "src"
if str(SRC_PATH) not in sys.path:
    sys.path.insert(0, str(SRC_PATH))

from pdf_ocr_poc.evaluation import evaluate_against_gold  # type: ignore[import-not-found]


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Evaluate OCR prediction against gold annotations"
    )
    parser.add_argument("--gold", required=True)
    parser.add_argument("--pred", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    result = evaluate_against_gold(
        gold_path=Path(args.gold),
        prediction_path=Path(args.pred),
    )

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as handle:
        json.dump(result, handle, ensure_ascii=False, indent=2)

    print(f"Saved evaluation: {out_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
