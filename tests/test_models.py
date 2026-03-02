from __future__ import annotations

from pdf_ocr_poc.models import BBox, OCRBlock, OCRDocument, OCRPage


def test_bbox_to_dict() -> None:
    bbox = BBox(1, 2, 3, 4)
    assert bbox.to_dict() == {"x0": 1, "y0": 2, "x1": 3, "y1": 4}


def test_ocr_page_text_respects_reading_order() -> None:
    blocks = [
        OCRBlock("second", BBox(0, 0, 1, 1), "paragraph", 90, 2),
        OCRBlock("first", BBox(0, 0, 1, 1), "paragraph", 90, 1),
    ]
    page = OCRPage(page=1, width=10, height=10, blocks=blocks)
    assert page.text == "first\nsecond"


def test_ocr_page_to_dict() -> None:
    block = OCRBlock("text", BBox(0, 0, 1, 1), "paragraph", 99, 1)
    page = OCRPage(page=1, width=100, height=200, blocks=[block])
    payload = page.to_dict()
    assert payload["page"] == 1
    assert payload["blocks"][0]["text"] == "text"


def test_ocr_document_text() -> None:
    p1 = OCRPage(
        page=1,
        width=10,
        height=10,
        blocks=[OCRBlock("a", BBox(0, 0, 1, 1), "paragraph", 90, 1)],
    )
    p2 = OCRPage(
        page=2,
        width=10,
        height=10,
        blocks=[OCRBlock("b", BBox(0, 0, 1, 1), "paragraph", 90, 1)],
    )
    doc = OCRDocument(engine="x", source_pdf="in.pdf", pages=[p1, p2])
    assert doc.text == "a\n\nb"


def test_ocr_document_to_dict_page_count() -> None:
    page = OCRPage(page=1, width=10, height=10, blocks=[])
    doc = OCRDocument(engine="x", source_pdf="in.pdf", pages=[page])
    payload = doc.to_dict()
    assert payload["page_count"] == 1
