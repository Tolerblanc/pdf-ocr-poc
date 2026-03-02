from __future__ import annotations

import math

from pdf_ocr_poc.metrics import (
    cer,
    code_line_accuracy,
    composite_score,
    levenshtein_distance,
    wer,
)


def test_levenshtein_distance_empty() -> None:
    assert levenshtein_distance([], []) == 0
    assert levenshtein_distance(["a"], []) == 1
    assert levenshtein_distance([], ["a", "b"]) == 2


def test_levenshtein_distance_basic() -> None:
    assert levenshtein_distance(list("kitten"), list("sitting")) == 3


def test_cer_exact_match() -> None:
    assert cer("안녕", "안녕") == 0.0


def test_cer_substitution() -> None:
    assert math.isclose(cer("abcd", "abxd"), 0.25)


def test_cer_with_normalization_space_collapse() -> None:
    assert cer("a  b", "a b") == 0.0


def test_wer_exact_match() -> None:
    assert wer("hello world", "hello world") == 0.0


def test_wer_insertion() -> None:
    assert math.isclose(wer("hello world", "hello brave world"), 0.5)


def test_code_line_accuracy_exact() -> None:
    ref = "a=1\nb=2"
    pred = "a=1\nb=2"
    assert code_line_accuracy(ref, pred) == 1.0


def test_code_line_accuracy_trailing_space_ignored() -> None:
    ref = "a=1\nreturn x"
    pred = "a=1   \nreturn x    "
    assert code_line_accuracy(ref, pred) == 1.0


def test_code_line_accuracy_partial() -> None:
    ref = "a=1\nb=2"
    pred = "a=1\nb=3"
    assert code_line_accuracy(ref, pred) == 0.5


def test_code_line_accuracy_tolerates_extra_noise_lines() -> None:
    ref = "a=1\nb=2"
    pred = "noise\na=1\nnoise2\nb=2"
    assert code_line_accuracy(ref, pred) == 1.0


def test_composite_score_monotonic_speed() -> None:
    baseline = composite_score(
        0.2, 0.8, 0.7, pages_per_minute=10, baseline_pages_per_minute=10
    )
    faster = composite_score(
        0.2, 0.8, 0.7, pages_per_minute=15, baseline_pages_per_minute=10
    )
    assert faster > baseline


def test_composite_score_monotonic_cer() -> None:
    worse = composite_score(
        0.3, 0.8, 0.7, pages_per_minute=10, baseline_pages_per_minute=10
    )
    better = composite_score(
        0.1, 0.8, 0.7, pages_per_minute=10, baseline_pages_per_minute=10
    )
    assert better > worse
