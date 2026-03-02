from __future__ import annotations

from pathlib import Path

import pytest

from pdf_ocr_poc.schema_validation import validate_page_payload


def _schema_path() -> Path:
    return Path(__file__).resolve().parents[1] / "schema" / "ocr-page-v1.json"


def test_validate_page_payload_valid() -> None:
    payload = {
        "page": 1,
        "width": 1000,
        "height": 1400,
        "is_blank": False,
        "text": "hello",
        "blocks": [
            {
                "text": "hello",
                "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
                "block_type": "paragraph",
                "confidence": 91.2,
                "reading_order": 1,
            }
        ],
    }
    validate_page_payload(payload, _schema_path())


def test_validate_page_payload_missing_field() -> None:
    payload = {
        "page": 1,
        "width": 1000,
        "height": 1400,
        "is_blank": False,
        "text": "hello",
        "blocks": [
            {
                "text": "hello",
                "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
                "block_type": "paragraph",
                "reading_order": 1,
            }
        ],
    }
    with pytest.raises(Exception):
        validate_page_payload(payload, _schema_path())


def test_validate_page_payload_invalid_block_type() -> None:
    payload = {
        "page": 1,
        "width": 1000,
        "height": 1400,
        "is_blank": False,
        "text": "hello",
        "blocks": [
            {
                "text": "hello",
                "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
                "block_type": "table",
                "confidence": 90,
                "reading_order": 1,
            }
        ],
    }
    with pytest.raises(Exception):
        validate_page_payload(payload, _schema_path())


def test_validate_page_payload_additional_field() -> None:
    payload = {
        "page": 1,
        "width": 1000,
        "height": 1400,
        "is_blank": False,
        "text": "hello",
        "blocks": [],
        "extra": "x",
    }
    with pytest.raises(Exception):
        validate_page_payload(payload, _schema_path())
