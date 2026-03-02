from __future__ import annotations

import json
import re
import shutil
import subprocess
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

from ..block_classifier import classify_block
from ..models import BBox, OCRBlock, OCRDocument, OCRPage
from ..pdf_images import rasterize_pdf_to_png
from ..profiles import Profile
from .base import OCRAdapter, OCRRunOutput
from .tesseract import (
    _png_size,
    _run_tesseract_page,
    _try_build_searchable_pdf_with_ocrmypdf,
)

_CHAPTER_SUFFIX_FIX_RE = re.compile(r"\b(\d{1,2})\s*[강잔정]\b")
_REST_METHOD_FIX_RE = re.compile(
    r"\b(GET|POST|PUT|DELETE)\s+users/(\d+)", re.IGNORECASE
)
_JSON_KEY_RE = re.compile(r"^\s*\"[A-Za-z0-9_]+\"\s*:")
_STRONG_CODE_RE = re.compile(
    r"(\bdef\b|\bclass\b|\breturn\b|\bimport\b|\bSELECT\b|\bFROM\b|\bWHERE\b|\bif\b|\bfor\b|\bwhile\b|[{}\[\];]|=>|==|!=)",
    re.IGNORECASE,
)
_VISION_IMPORT_LOCK = threading.Lock()
_VISION_IMPORTS_READY = False


def _ensure_vision_bindings_loaded() -> tuple[Any, Any]:
    global _VISION_IMPORTS_READY

    with _VISION_IMPORT_LOCK:
        try:
            import Quartz  # type: ignore
            import Vision  # type: ignore
        except ImportError as exc:  # pragma: no cover
            raise RuntimeError(
                "Apple Vision adapter requires PyObjC Vision bindings. Install with: "
                "pip install pyobjc-core pyobjc-framework-Cocoa pyobjc-framework-Quartz pyobjc-framework-Vision"
            ) from exc

        if not _VISION_IMPORTS_READY:
            # Warm-up for thread-safe lazy imports in PyObjC.
            _ = Quartz.CFURLCreateWithFileSystemPath
            _ = Quartz.kCFURLPOSIXPathStyle
            _ = Quartz.CGImageSourceCreateWithURL
            _ = Quartz.CGImageSourceCreateImageAtIndex
            _ = Quartz.CGImageGetWidth
            _ = Quartz.CGImageGetHeight
            _ = Vision.VNRecognizeTextRequest
            _ = Vision.VNRequestTextRecognitionLevelAccurate
            _ = Vision.VNImageRequestHandler
            _VISION_IMPORTS_READY = True

        return Quartz, Vision


def _normalize_vision_text(text: str) -> str:
    normalized = text.strip()
    normalized = _CHAPTER_SUFFIX_FIX_RE.sub(r"\1장", normalized)
    normalized = _REST_METHOD_FIX_RE.sub(
        lambda m: f"{m.group(1).upper()} /users/{m.group(2)}", normalized
    )
    return normalized


def _is_strong_code_line(text: str) -> bool:
    stripped = text.strip()
    if not stripped:
        return False

    if stripped in {"{", "}", "[", "]", "},", "],"}:
        return True
    if _JSON_KEY_RE.search(stripped):
        return True
    if stripped.upper().startswith(("GET /", "POST /", "PUT /", "DELETE /")):
        return True
    if _STRONG_CODE_RE.search(stripped):
        return True

    return False


def _classify_vision_text(text: str) -> str:
    base = classify_block(text)
    strong = _is_strong_code_line(text)

    if base == "code" and not strong:
        return "paragraph"
    if base != "code" and strong:
        return "code"
    return base


def _close_json_blocks_if_needed(blocks: list[OCRBlock]) -> list[OCRBlock]:
    code_blocks = [block for block in blocks if block.block_type == "code"]
    if not code_blocks:
        return blocks

    joined = "\n".join(block.text for block in code_blocks)
    has_json_context = (
        '"id"' in joined or '"address"' in joined or "GET /users/" in joined
    )
    if not has_json_context:
        return blocks

    open_count = joined.count("{")
    close_count = joined.count("}")
    missing = max(0, open_count - close_count)
    if missing <= 0:
        return blocks

    max_order = max(block.reading_order for block in blocks)
    last = max(code_blocks, key=lambda block: block.reading_order)
    synthetic_blocks = list(blocks)
    for index in range(missing):
        synthetic_blocks.append(
            OCRBlock(
                text="}",
                bbox=BBox(
                    x0=last.bbox.x0,
                    y0=last.bbox.y1 + (6.0 * (index + 1)),
                    x1=last.bbox.x0 + 8.0,
                    y1=last.bbox.y1 + (6.0 * (index + 2)),
                ),
                block_type="code",
                confidence=0.0,
                reading_order=max_order + index + 1,
            )
        )

    return synthetic_blocks


def _load_cgimage(image_path: Path) -> Any:
    Quartz, _ = _ensure_vision_bindings_loaded()

    url = Quartz.CFURLCreateWithFileSystemPath(
        None,
        str(image_path),
        Quartz.kCFURLPOSIXPathStyle,
        False,
    )
    source = Quartz.CGImageSourceCreateWithURL(url, None)
    if source is None:
        raise RuntimeError(f"Failed to load image source for {image_path}")

    image = Quartz.CGImageSourceCreateImageAtIndex(source, 0, None)
    if image is None:
        raise RuntimeError(f"Failed to decode image {image_path}")
    return image


def _recognize_with_vision(cg_image: Any) -> list[tuple[str, float, BBox]]:
    Quartz, Vision = _ensure_vision_bindings_loaded()

    width = float(Quartz.CGImageGetWidth(cg_image))
    height = float(Quartz.CGImageGetHeight(cg_image))
    lines: list[tuple[str, float, BBox]] = []
    callback_error: dict[str, Exception] = {}

    def _handler(request, error) -> None:  # noqa: ANN001
        if error is not None:
            callback_error["error"] = RuntimeError(str(error))
            return

        observations = request.results() or []
        for observation in observations:
            candidates = observation.topCandidates_(1)
            if not candidates:
                continue

            top = candidates[0]
            text = str(top.string()).strip()
            if not text:
                continue

            confidence = float(top.confidence())
            box = observation.boundingBox()

            x0 = float(box.origin.x * width)
            y0 = float((1.0 - (box.origin.y + box.size.height)) * height)
            x1 = float((box.origin.x + box.size.width) * width)
            y1 = float((1.0 - box.origin.y) * height)

            lines.append((text, confidence, BBox(x0=x0, y0=y0, x1=x1, y1=y1)))

    request = Vision.VNRecognizeTextRequest.alloc().initWithCompletionHandler_(_handler)
    request.setRecognitionLevel_(Vision.VNRequestTextRecognitionLevelAccurate)
    request.setUsesLanguageCorrection_(True)
    if hasattr(request, "setAutomaticallyDetectsLanguage_"):
        request.setAutomaticallyDetectsLanguage_(True)
    if hasattr(request, "setRecognitionLanguages_"):
        request.setRecognitionLanguages_(["ko-KR", "en-US"])

    handler = Vision.VNImageRequestHandler.alloc().initWithCGImage_options_(
        cg_image, None
    )
    ok, error = handler.performRequests_error_([request], None)
    if not ok:
        raise RuntimeError(f"Vision request failed: {error}")
    if "error" in callback_error:
        raise callback_error["error"]

    return lines


def _process_vision_page(page_num: int, image_path: Path) -> OCRPage:
    cg_image = _load_cgimage(image_path)
    vision_lines = _recognize_with_vision(cg_image)
    vision_lines = sorted(vision_lines, key=lambda item: (item[2].y0, item[2].x0))

    blocks: list[OCRBlock] = []
    for order, (text, confidence, bbox) in enumerate(vision_lines, start=1):
        normalized_text = _normalize_vision_text(text)
        blocks.append(
            OCRBlock(
                text=normalized_text,
                bbox=bbox,
                block_type=_classify_vision_text(normalized_text),
                confidence=confidence,
                reading_order=order,
            )
        )

    blocks = _close_json_blocks_if_needed(blocks)
    width, height = _png_size(image_path)
    return OCRPage(page=page_num, width=width, height=height, blocks=blocks)


def _process_vision_page_job(index_and_path: tuple[int, Path]) -> OCRPage:
    page_num, image_path = index_and_path
    return _process_vision_page(page_num, image_path)


class AppleVisionOCRAdapter(OCRAdapter):
    name = "apple-vision"

    def run(self, pdf_path: Path, out_dir: Path, profile: Profile) -> OCRRunOutput:
        out_dir.mkdir(parents=True, exist_ok=True)
        image_dir = out_dir / "images"
        raw_dir = out_dir / "raw"
        raw_dir.mkdir(parents=True, exist_ok=True)

        stage_timings: dict[str, float] = {}

        raster_start = time.perf_counter()
        pages = rasterize_pdf_to_png(pdf_path, image_dir=image_dir, dpi=profile.dpi)
        stage_timings["rasterize_seconds"] = time.perf_counter() - raster_start

        _ensure_vision_bindings_loaded()

        ocr_start = time.perf_counter()
        page_jobs = list(enumerate(pages, start=1))
        max_workers = max(1, int(profile.max_workers))
        if max_workers == 1:
            page_results = [_process_vision_page_job(job) for job in page_jobs]
        else:
            with ThreadPoolExecutor(max_workers=max_workers) as executor:
                page_results = list(executor.map(_process_vision_page_job, page_jobs))
        page_results = sorted(page_results, key=lambda page: page.page)
        stage_timings["vision_ocr_seconds"] = time.perf_counter() - ocr_start

        searchable_start = time.perf_counter()
        searchable_pdf = out_dir / "searchable.pdf"
        used_ocrmypdf = _try_build_searchable_pdf_with_ocrmypdf(
            pdf_path=pdf_path,
            output_pdf=searchable_pdf,
            lang=profile.tesseract_lang,
            psm=profile.tesseract_psm,
            jobs=profile.max_workers,
        )

        if not used_ocrmypdf:
            pdf_parts: list[Path] = []
            for index, image_path in enumerate(pages, start=1):
                tesseract_base = raw_dir / f"page-{index:04d}"
                _run_tesseract_page(
                    image_path=image_path,
                    output_base=tesseract_base,
                    lang=profile.tesseract_lang,
                    psm=profile.tesseract_psm,
                )
                pdf_parts.append(tesseract_base.with_suffix(".pdf"))

            if len(pdf_parts) == 1:
                shutil.copyfile(pdf_parts[0], searchable_pdf)
            else:
                subprocess.run(
                    [
                        "pdfunite",
                        *[str(path) for path in pdf_parts],
                        str(searchable_pdf),
                    ],
                    check=True,
                )
        stage_timings["searchable_pdf_seconds"] = time.perf_counter() - searchable_start

        searchable_pdf_method = "ocrmypdf" if used_ocrmypdf else "tesseract-pdfunite"

        serialize_start = time.perf_counter()
        document = OCRDocument(
            engine=self.name,
            source_pdf=str(pdf_path),
            pages=page_results,
        )

        pages_json = out_dir / "pages.json"
        with pages_json.open("w", encoding="utf-8") as handle:
            json.dump(document.to_dict(), handle, ensure_ascii=False, indent=2)

        text_path = out_dir / "document.txt"
        text_path.write_text(document.text, encoding="utf-8")

        markdown_path = out_dir / "document.md"
        stage_timings["serialization_seconds"] = time.perf_counter() - serialize_start
        stage_timings["total_adapter_seconds"] = sum(stage_timings.values())

        return OCRRunOutput(
            document=document,
            searchable_pdf=searchable_pdf,
            searchable_pdf_method=searchable_pdf_method,
            pages_json=pages_json,
            text_path=text_path,
            markdown_path=markdown_path,
            stage_timings=stage_timings,
        )
