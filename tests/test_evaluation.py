from __future__ import annotations

import json
from pathlib import Path

from pdf_ocr_poc.evaluation import evaluate_against_gold


def _write(path: Path, payload: dict) -> None:
    path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")


def test_evaluate_against_gold_basic(tmp_path: Path) -> None:
    gold = {
        "version": "v1",
        "pages": [
            {
                "page": 1,
                "prose_kr": "안녕하세요",
                "expected_block_types": ["paragraph"],
                "reading_order_snippets": ["안녕하세요"],
            }
        ],
    }
    pred = {
        "pages": [
            {
                "page": 1,
                "text": "안녕하세요",
                "blocks": [
                    {
                        "text": "안녕하세요",
                        "block_type": "paragraph",
                    }
                ],
            }
        ]
    }
    gold_path = tmp_path / "gold.json"
    pred_path = tmp_path / "pred.json"
    _write(gold_path, gold)
    _write(pred_path, pred)

    result = evaluate_against_gold(gold_path, pred_path)
    assert result["summary"]["kr_prose_cer"] == 0.0
    assert result["summary"]["layout_macro_f1"] == 1.0
    assert result["summary"]["reading_order_error_ratio"] == 0.0


def test_evaluate_against_gold_missing_prediction(tmp_path: Path) -> None:
    gold = {"version": "v1", "pages": [{"page": 1, "prose_kr": "abc"}]}
    pred = {"pages": []}
    gold_path = tmp_path / "gold.json"
    pred_path = tmp_path / "pred.json"
    _write(gold_path, gold)
    _write(pred_path, pred)

    result = evaluate_against_gold(gold_path, pred_path)
    assert result["per_page"][1]["missing_prediction"]


def test_evaluate_against_gold_code_metric(tmp_path: Path) -> None:
    gold = {
        "version": "v1",
        "pages": [
            {
                "page": 1,
                "code": "a=1\nb=2",
            }
        ],
    }
    pred = {
        "pages": [
            {
                "page": 1,
                "text": "a=1\nb=2",
                "blocks": [
                    {"text": "a=1", "block_type": "code"},
                    {"text": "b=3", "block_type": "code"},
                ],
            }
        ]
    }
    gold_path = tmp_path / "gold.json"
    pred_path = tmp_path / "pred.json"
    _write(gold_path, gold)
    _write(pred_path, pred)

    result = evaluate_against_gold(gold_path, pred_path)
    assert result["summary"]["code_line_accuracy"] == 0.5


def test_evaluate_against_gold_reading_order_error(tmp_path: Path) -> None:
    gold = {
        "version": "v1",
        "pages": [{"page": 1, "reading_order_snippets": ["B", "A"]}],
    }
    pred = {
        "pages": [{"page": 1, "text": "A ... B", "blocks": []}],
    }
    gold_path = tmp_path / "gold.json"
    pred_path = tmp_path / "pred.json"
    _write(gold_path, gold)
    _write(pred_path, pred)

    result = evaluate_against_gold(gold_path, pred_path)
    assert result["summary"]["reading_order_error_ratio"] == 1.0


def test_evaluate_against_gold_reading_order_normalizes_punctuation(
    tmp_path: Path,
) -> None:
    gold = {
        "version": "v1",
        "pages": [{"page": 1, "reading_order_snippets": ["GET /users/12", "JSON"]}],
    }
    pred = {
        "pages": [{"page": 1, "text": "GET users/12 then JSON", "blocks": []}],
    }
    gold_path = tmp_path / "gold.json"
    pred_path = tmp_path / "pred.json"
    _write(gold_path, gold)
    _write(pred_path, pred)

    result = evaluate_against_gold(gold_path, pred_path)
    assert result["summary"]["reading_order_error_ratio"] == 0.0
