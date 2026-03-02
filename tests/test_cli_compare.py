from __future__ import annotations

import json
from pathlib import Path

from pdf_ocr_poc.cli import _effective_cer


def test_effective_cer_prefers_available_values() -> None:
    assert _effective_cer({"kr_prose_cer": 0.1, "mixed_prose_cer": 0.3}) == 0.2
    assert _effective_cer({"kr_prose_cer": 0.2, "mixed_prose_cer": None}) == 0.2


def test_effective_cer_no_values() -> None:
    assert _effective_cer({}) == 1.0


def test_compare_payload_shape(tmp_path: Path) -> None:
    baseline_eval = tmp_path / "b_eval.json"
    candidate_eval = tmp_path / "c_eval.json"
    baseline_report = tmp_path / "b_report.json"
    candidate_report = tmp_path / "c_report.json"

    baseline_eval.write_text(
        json.dumps(
            {
                "summary": {
                    "kr_prose_cer": 0.3,
                    "code_line_accuracy": 0.5,
                    "layout_macro_f1": 0.4,
                }
            }
        ),
        encoding="utf-8",
    )
    candidate_eval.write_text(
        json.dumps(
            {
                "summary": {
                    "kr_prose_cer": 0.2,
                    "code_line_accuracy": 0.6,
                    "layout_macro_f1": 0.5,
                }
            }
        ),
        encoding="utf-8",
    )
    baseline_report.write_text(json.dumps({"pages_per_minute": 10}), encoding="utf-8")
    candidate_report.write_text(json.dumps({"pages_per_minute": 12}), encoding="utf-8")

    assert baseline_eval.exists()
    assert candidate_eval.exists()
    assert baseline_report.exists()
    assert candidate_report.exists()
