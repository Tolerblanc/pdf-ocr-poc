from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .metrics import cer, code_line_accuracy
from .normalization import normalize_prose


@dataclass(slots=True)
class EvaluationSummary:
    kr_prose_cer: float | None
    mixed_prose_cer: float | None
    code_line_accuracy: float | None
    layout_macro_f1: float | None
    reading_order_error_ratio: float | None

    def to_dict(self) -> dict[str, float | None]:
        return {
            "kr_prose_cer": self.kr_prose_cer,
            "mixed_prose_cer": self.mixed_prose_cer,
            "code_line_accuracy": self.code_line_accuracy,
            "layout_macro_f1": self.layout_macro_f1,
            "reading_order_error_ratio": self.reading_order_error_ratio,
        }


def _load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def _f1(expected: set[str], actual: set[str]) -> float:
    if not expected and not actual:
        return 1.0
    if not expected or not actual:
        return 0.0
    tp = len(expected.intersection(actual))
    precision = tp / len(actual)
    recall = tp / len(expected)
    if precision + recall == 0:
        return 0.0
    return (2 * precision * recall) / (precision + recall)


def _best_snippet_cer(reference: str, prediction_text: str) -> float:
    ref = normalize_prose(reference)
    pred = normalize_prose(prediction_text)
    if not ref:
        return 0.0
    if not pred:
        return 1.0
    if ref in pred:
        return 0.0

    candidate_lines = [line for line in pred.splitlines() if line.strip()]
    if not candidate_lines:
        return 1.0

    scores = [cer(ref, line) for line in candidate_lines]
    return min(scores)


def _normalized_snippet_positions(text: str, snippets: list[str]) -> list[int]:
    def _normalize_for_search(value: str) -> str:
        compact = normalize_prose(value).lower()
        return "".join(re.findall(r"[0-9a-z가-힣]+", compact))

    normalized_text = _normalize_for_search(text)
    positions: list[int] = []
    for snippet in snippets:
        normalized_snippet = _normalize_for_search(snippet)
        positions.append(normalized_text.find(normalized_snippet))
    return positions


def evaluate_against_gold(gold_path: Path, prediction_path: Path) -> dict[str, Any]:
    gold = _load_json(gold_path)
    pred = _load_json(prediction_path)

    pred_pages = {int(page["page"]): page for page in pred.get("pages", [])}

    kr_samples: list[float] = []
    mixed_samples: list[float] = []
    code_samples: list[float] = []
    layout_samples: list[float] = []
    reading_order_errors = 0
    reading_order_total = 0

    per_page: dict[int, dict[str, Any]] = {}

    for gold_page in gold.get("pages", []):
        page_num = int(gold_page["page"])
        pred_page = pred_pages.get(page_num)
        if pred_page is None:
            per_page[page_num] = {"missing_prediction": True}
            continue

        page_metrics: dict[str, Any] = {}
        pred_text = str(pred_page.get("text", ""))

        prose_kr = str(gold_page.get("prose_kr", "") or "")
        if prose_kr:
            value = _best_snippet_cer(prose_kr, pred_text)
            kr_samples.append(value)
            page_metrics["kr_prose_cer"] = value

        prose_mixed = str(gold_page.get("prose_mixed", "") or "")
        if prose_mixed:
            value = _best_snippet_cer(prose_mixed, pred_text)
            mixed_samples.append(value)
            page_metrics["mixed_prose_cer"] = value

        code_ref = str(gold_page.get("code", "") or "")
        if code_ref:
            code_pred_lines = [
                block.get("text", "")
                for block in pred_page.get("blocks", [])
                if block.get("block_type") == "code"
            ]
            code_pred = "\n".join(code_pred_lines)
            value = code_line_accuracy(code_ref, code_pred)
            code_samples.append(value)
            page_metrics["code_line_accuracy"] = value

        expected_types = set(gold_page.get("expected_block_types", []) or [])
        if expected_types:
            actual_types = {
                str(block.get("block_type", "paragraph"))
                for block in pred_page.get("blocks", [])
            }
            value = _f1(expected_types, actual_types)
            layout_samples.append(value)
            page_metrics["layout_f1"] = value

        snippets = list(gold_page.get("reading_order_snippets", []) or [])
        if snippets:
            reading_order_total += 1
            positions = _normalized_snippet_positions(pred_text, snippets)
            if any(pos < 0 for pos in positions):
                reading_order_errors += 1
                page_metrics["reading_order_ok"] = False
            else:
                in_order = all(
                    positions[i] < positions[i + 1] for i in range(len(positions) - 1)
                )
                if not in_order:
                    reading_order_errors += 1
                page_metrics["reading_order_ok"] = in_order

        per_page[page_num] = page_metrics

    summary = EvaluationSummary(
        kr_prose_cer=(sum(kr_samples) / len(kr_samples)) if kr_samples else None,
        mixed_prose_cer=(sum(mixed_samples) / len(mixed_samples))
        if mixed_samples
        else None,
        code_line_accuracy=(sum(code_samples) / len(code_samples))
        if code_samples
        else None,
        layout_macro_f1=(sum(layout_samples) / len(layout_samples))
        if layout_samples
        else None,
        reading_order_error_ratio=(reading_order_errors / reading_order_total)
        if reading_order_total
        else None,
    )

    return {
        "summary": summary.to_dict(),
        "per_page": per_page,
        "meta": {
            "gold_version": gold.get("version", "unknown"),
            "evaluated_pages": len(gold.get("pages", [])),
        },
    }
