from __future__ import annotations

import pytest

from pdf_ocr_poc.normalization import (
    normalize_code,
    normalize_prose,
    normalize_unicode,
    tokenize_for_wer,
)


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("ABC", "ABC"),
        ("Cafe\u0301", "Café"),
        ("한\u1100\u1161", "한가"),
    ],
)
def test_normalize_unicode(raw: str, expected: str) -> None:
    assert normalize_unicode(raw) == expected


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("a    b", "a b"),
        ("a\tb", "a b"),
        ("  leading and trailing  ", "leading and trailing"),
        ("line1  \nline2\t\t", "line1\nline2"),
        ("punctuation,kept!", "punctuation,kept!"),
        ("", ""),
    ],
)
def test_normalize_prose(raw: str, expected: str) -> None:
    assert normalize_prose(raw) == expected


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("a    b", "a    b"),
        ("a\tb", "a\tb"),
        ("line1   \nline2   ", "line1\nline2"),
        ("\n\ncode\n", "code"),
    ],
)
def test_normalize_code(raw: str, expected: str) -> None:
    assert normalize_code(raw) == expected


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("", []),
        ("hello world", ["hello", "world"]),
        ("a   b\n c", ["a", "b", "c"]),
        ("한글  OCR", ["한글", "OCR"]),
    ],
)
def test_tokenize_for_wer(raw: str, expected: list[str]) -> None:
    assert tokenize_for_wer(raw) == expected
