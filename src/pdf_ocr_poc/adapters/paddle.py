from __future__ import annotations

import json
import os
import shutil
import subprocess
from pathlib import Path
from typing import Any

from ..block_classifier import classify_block
from ..models import BBox, OCRBlock, OCRDocument, OCRPage
from ..pdf_images import rasterize_pdf_to_png
from ..profiles import Profile
from .base import OCRAdapter, OCRRunOutput
from .tesseract import _png_size, _run_tesseract_page


def _bbox_from_polygon(poly: list[list[float]]) -> BBox:
    xs = [float(point[0]) for point in poly]
    ys = [float(point[1]) for point in poly]
    return BBox(
        x0=float(min(xs)), y0=float(min(ys)), x1=float(max(xs)), y1=float(max(ys))
    )


def _extract_paddle_lines(raw_result: Any) -> list[tuple[str, float, BBox]]:
    lines: list[tuple[str, float, BBox]] = []

    # PaddleOCR 3.x object is dict-like with rec_texts/rec_scores/rec_polys.
    if isinstance(raw_result, dict) and "rec_texts" in raw_result:
        texts = list(raw_result.get("rec_texts") or [])
        scores = list(raw_result.get("rec_scores") or [])
        polys = list(raw_result.get("rec_polys") or [])
        for idx, text in enumerate(texts):
            poly = polys[idx] if idx < len(polys) else None
            score = float(scores[idx]) if idx < len(scores) else 0.0
            if poly is None:
                continue
            bbox = _bbox_from_polygon(poly)
            lines.append((str(text), score, bbox))
        return lines

    # Common old API: [ [ [poly], (text, conf) ], ... ]
    if isinstance(raw_result, list):
        for entry in raw_result:
            if not isinstance(entry, list) or len(entry) < 2:
                continue
            poly = entry[0]
            payload = entry[1]
            if not isinstance(poly, list) or not isinstance(payload, (list, tuple)):
                continue
            if len(payload) < 2:
                continue
            text = str(payload[0])
            conf = float(payload[1])
            bbox = _bbox_from_polygon(poly)
            lines.append((text, conf, bbox))
        return lines

    return lines


class PaddleOCRAdapter(OCRAdapter):
    name = "paddle"

    def run(self, pdf_path: Path, out_dir: Path, profile: Profile) -> OCRRunOutput:
        os.environ.setdefault("PADDLE_PDX_DISABLE_MODEL_SOURCE_CHECK", "True")

        try:
            from paddleocr import PaddleOCR  # type: ignore
        except ImportError as exc:  # pragma: no cover
            raise RuntimeError(
                "PaddleOCR is not installed. Install with: pip install -e .[paddle]"
            ) from exc

        out_dir.mkdir(parents=True, exist_ok=True)
        image_dir = out_dir / "images"
        raw_dir = out_dir / "raw"
        raw_dir.mkdir(parents=True, exist_ok=True)

        pages = rasterize_pdf_to_png(pdf_path, image_dir=image_dir, dpi=profile.dpi)

        try:
            ocr = PaddleOCR(
                lang="korean",
                use_doc_orientation_classify=False,
                use_doc_unwarping=False,
                use_textline_orientation=False,
                text_detection_model_name="PP-OCRv5_mobile_det",
                text_recognition_model_name="korean_PP-OCRv5_mobile_rec",
            )
        except TypeError:
            ocr = PaddleOCR(lang="korean")

        page_results: list[OCRPage] = []
        pdf_parts: list[Path] = []

        for index, image_path in enumerate(pages, start=1):
            if hasattr(ocr, "predict"):
                raw = ocr.predict(input=str(image_path))
            else:
                raw = ocr.ocr(str(image_path), cls=False)

            page_raw = raw[0] if isinstance(raw, list) and raw else raw
            lines = _extract_paddle_lines(page_raw)
            lines = sorted(lines, key=lambda item: (item[2].y0, item[2].x0))

            blocks: list[OCRBlock] = []
            for order, (text, conf, bbox) in enumerate(lines, start=1):
                blocks.append(
                    OCRBlock(
                        text=text,
                        bbox=bbox,
                        block_type=classify_block(text),
                        confidence=conf,
                        reading_order=order,
                    )
                )

            width, height = _png_size(image_path)
            page_results.append(
                OCRPage(page=index, width=width, height=height, blocks=blocks)
            )

            # Searchable PDF fallback generation is delegated to local Tesseract.
            tesseract_base = raw_dir / f"page-{index:04d}"
            _run_tesseract_page(
                image_path=image_path,
                output_base=tesseract_base,
                lang=profile.tesseract_lang,
                psm=profile.tesseract_psm,
            )
            pdf_parts.append(tesseract_base.with_suffix(".pdf"))

        searchable_pdf = out_dir / "searchable.pdf"
        if len(pdf_parts) == 1:
            shutil.copyfile(pdf_parts[0], searchable_pdf)
        else:
            subprocess.run(
                ["pdfunite", *[str(path) for path in pdf_parts], str(searchable_pdf)],
                check=True,
            )

        document = OCRDocument(
            engine=self.name, source_pdf=str(pdf_path), pages=page_results
        )

        pages_json = out_dir / "pages.json"
        with pages_json.open("w", encoding="utf-8") as handle:
            json.dump(document.to_dict(), handle, ensure_ascii=False, indent=2)

        text_path = out_dir / "document.txt"
        text_path.write_text(document.text, encoding="utf-8")
        markdown_path = out_dir / "document.md"

        return OCRRunOutput(
            document=document,
            searchable_pdf=searchable_pdf,
            searchable_pdf_method="tesseract-pdfunite",
            pages_json=pages_json,
            text_path=text_path,
            markdown_path=markdown_path,
        )
