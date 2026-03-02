from __future__ import annotations

from pdf_ocr_poc.adapters.apple_vision import (
    _classify_vision_text,
    _close_json_blocks_if_needed,
    _normalize_vision_text,
)
from pdf_ocr_poc.models import BBox, OCRBlock


def test_normalize_vision_text_chapter_suffix_fix() -> None:
    assert _normalize_vision_text("3강 시스템 설계") == "3장 시스템 설계"
    assert _normalize_vision_text("11잔") == "11장"


def test_normalize_vision_text_rest_path_fix() -> None:
    assert _normalize_vision_text("GET users/12") == "GET /users/12"


def test_classify_vision_text_demotes_false_code() -> None:
    assert _classify_vision_text("variation가 많아서다.") == "paragraph"


def test_classify_vision_text_json_key_is_code() -> None:
    assert _classify_vision_text('"id": 12,') == "code"


def test_close_json_blocks_if_needed_adds_brace() -> None:
    blocks = [
        OCRBlock(
            text="GET /users/12",
            bbox=BBox(0, 0, 1, 1),
            block_type="code",
            confidence=1.0,
            reading_order=1,
        ),
        OCRBlock(
            text="{",
            bbox=BBox(0, 2, 1, 3),
            block_type="code",
            confidence=1.0,
            reading_order=2,
        ),
        OCRBlock(
            text='"id": 12,',
            bbox=BBox(0, 4, 1, 5),
            block_type="code",
            confidence=1.0,
            reading_order=3,
        ),
    ]
    fixed = _close_json_blocks_if_needed(blocks)
    assert fixed[-1].text == "}"
    assert fixed[-1].reading_order == 4
