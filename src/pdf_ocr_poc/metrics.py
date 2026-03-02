from __future__ import annotations

from collections.abc import Callable, Sequence
import re

from .normalization import normalize_code, normalize_prose, tokenize_for_wer


def levenshtein_distance(a: Sequence[str], b: Sequence[str]) -> int:
    if not a:
        return len(b)
    if not b:
        return len(a)

    prev = list(range(len(b) + 1))
    for i, ach in enumerate(a, start=1):
        curr = [i]
        for j, bch in enumerate(b, start=1):
            insert_cost = curr[j - 1] + 1
            delete_cost = prev[j] + 1
            replace_cost = prev[j - 1] + (0 if ach == bch else 1)
            curr.append(min(insert_cost, delete_cost, replace_cost))
        prev = curr
    return prev[-1]


def cer(
    reference: str,
    prediction: str,
    normalizer: Callable[[str], str] = normalize_prose,
) -> float:
    ref = normalizer(reference)
    pred = normalizer(prediction)
    if not ref:
        return 0.0 if not pred else 1.0
    return levenshtein_distance(list(ref), list(pred)) / len(ref)


def wer(
    reference: str,
    prediction: str,
    tokenizer: Callable[[str], list[str]] = tokenize_for_wer,
) -> float:
    ref_tokens = tokenizer(reference)
    pred_tokens = tokenizer(prediction)
    if not ref_tokens:
        return 0.0 if not pred_tokens else 1.0
    return levenshtein_distance(ref_tokens, pred_tokens) / len(ref_tokens)


def code_line_accuracy(reference: str, prediction: str) -> float:
    def _normalize_code_line(line: str) -> str:
        line = line.rstrip()
        line = re.sub(r"\s+", " ", line)
        line = re.sub(r"\s*([{}\[\],:])\s*", r"\1", line)
        line = re.sub(r"\s*/\s*", "/", line)
        return line.strip()

    ref_lines = [
        _normalize_code_line(line)
        for line in normalize_code(reference).splitlines()
        if line.strip()
    ]
    pred_lines = [
        _normalize_code_line(line)
        for line in normalize_code(prediction).splitlines()
        if line.strip()
    ]

    if not ref_lines:
        return 0.0 if pred_lines else 1.0

    # Use LCS to tolerate extra noisy lines while preserving order.
    rows = len(ref_lines)
    cols = len(pred_lines)
    dp = [[0] * (cols + 1) for _ in range(rows + 1)]
    for i in range(1, rows + 1):
        for j in range(1, cols + 1):
            if ref_lines[i - 1] == pred_lines[j - 1]:
                dp[i][j] = dp[i - 1][j - 1] + 1
            else:
                dp[i][j] = max(dp[i - 1][j], dp[i][j - 1])

    matches = dp[rows][cols]
    return matches / len(ref_lines)


def composite_score(
    cer_score: float,
    code_acc: float,
    layout_f1: float,
    pages_per_minute: float,
    baseline_pages_per_minute: float,
) -> float:
    # Lower CER is better -> convert to quality score.
    cer_quality = max(0.0, 1.0 - cer_score)

    # Throughput normalized against baseline throughput.
    if baseline_pages_per_minute <= 0:
        speed_quality = 0.0
    else:
        speed_quality = min(2.0, pages_per_minute / baseline_pages_per_minute)

    return (
        cer_quality * 0.40 + code_acc * 0.25 + layout_f1 * 0.20 + speed_quality * 0.15
    )
