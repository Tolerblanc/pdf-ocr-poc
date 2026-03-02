from __future__ import annotations

import pytest

from pdf_ocr_poc.pipeline import _adapter_for


def test_adapter_for_tesseract() -> None:
    adapter = _adapter_for("tesseract")
    assert adapter.name == "tesseract"


def test_adapter_for_paddle() -> None:
    adapter = _adapter_for("paddle")
    assert adapter.name == "paddle"


def test_adapter_for_apple_vision() -> None:
    adapter = _adapter_for("apple-vision")
    assert adapter.name == "apple-vision"


def test_adapter_for_vision_alias() -> None:
    adapter = _adapter_for("vision")
    assert adapter.name == "apple-vision"


def test_adapter_for_invalid() -> None:
    with pytest.raises(ValueError):
        _adapter_for("unknown")
