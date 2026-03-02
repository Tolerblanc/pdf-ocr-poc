from __future__ import annotations

import pytest

from pdf_ocr_poc.block_classifier import classify_block


@pytest.mark.parametrize(
    ("text", "expected"),
    [
        ("1장 사용자 수에 따른 규모 확장성", "heading"),
        ("Chapter 3 System Design", "heading"),
        ("그림 1-1", "caption"),
        ("Figure 3.2", "caption"),
        ("def hello():", "code"),
        ("SELECT * FROM users;", "code"),
        ("if (x > 0) {", "code"),
        ("이 문단은 일반 설명 문장이다.", "paragraph"),
    ],
)
def test_classify_block(text: str, expected: str) -> None:
    assert classify_block(text) == expected
