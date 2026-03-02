from __future__ import annotations

import csv
import json
import shutil
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

from ..block_classifier import classify_block
from ..models import BBox, OCRBlock, OCRDocument, OCRPage
from ..pdf_images import rasterize_pdf_to_png
from ..profiles import Profile
from .base import OCRAdapter, OCRRunOutput


def _png_size(path: Path) -> tuple[int, int]:
    with path.open("rb") as handle:
        data = handle.read(24)
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n":
        return 0, 0
    width = int.from_bytes(data[16:20], "big")
    height = int.from_bytes(data[20:24], "big")
    return width, height


@dataclass(slots=True)
class _LineAggregate:
    words: list[str]
    confs: list[float]
    x0: int
    y0: int
    x1: int
    y1: int


def _parse_tsv(tsv_path: Path) -> list[OCRBlock]:
    lines: dict[tuple[int, int, int], _LineAggregate] = {}

    with tsv_path.open("r", encoding="utf-8") as handle:
        reader = csv.DictReader(handle, delimiter="\t")
        for row in reader:
            text = (row.get("text") or "").strip()
            if not text:
                continue

            level = int(row.get("level") or 0)
            if level != 5:  # word level
                continue

            block_num = int(row.get("block_num") or 0)
            par_num = int(row.get("par_num") or 0)
            line_num = int(row.get("line_num") or 0)
            key = (block_num, par_num, line_num)

            left = int(float(row.get("left") or 0))
            top = int(float(row.get("top") or 0))
            width = int(float(row.get("width") or 0))
            height = int(float(row.get("height") or 0))
            conf = float(row.get("conf") or 0)

            aggregate = lines.get(key)
            if aggregate is None:
                aggregate = _LineAggregate(
                    words=[],
                    confs=[],
                    x0=left,
                    y0=top,
                    x1=left + width,
                    y1=top + height,
                )
                lines[key] = aggregate
            else:
                aggregate.x0 = min(aggregate.x0, left)
                aggregate.y0 = min(aggregate.y0, top)
                aggregate.x1 = max(aggregate.x1, left + width)
                aggregate.y1 = max(aggregate.y1, top + height)

            aggregate.words.append(text)
            if conf >= 0:
                aggregate.confs.append(conf)

    blocks: list[OCRBlock] = []
    sortable = sorted(lines.items(), key=lambda item: (item[1].y0, item[1].x0))
    for order, (_, aggregate) in enumerate(sortable, start=1):
        text = " ".join(aggregate.words)
        confidence = (
            sum(aggregate.confs) / len(aggregate.confs) if aggregate.confs else 0.0
        )
        blocks.append(
            OCRBlock(
                text=text,
                bbox=BBox(
                    x0=float(aggregate.x0),
                    y0=float(aggregate.y0),
                    x1=float(aggregate.x1),
                    y1=float(aggregate.y1),
                ),
                block_type=classify_block(text),
                confidence=confidence,
                reading_order=order,
            )
        )
    return blocks


def _run_cmd(cmd: list[str]) -> None:
    subprocess.run(cmd, check=True)


def _run_tesseract_page(
    image_path: Path, output_base: Path, lang: str, psm: int
) -> None:
    base_cmd = [
        "tesseract",
        str(image_path),
        str(output_base),
        "-l",
        lang,
        "--psm",
        str(psm),
    ]
    _run_cmd(base_cmd + ["txt"])
    _run_cmd(base_cmd + ["tsv"])
    _run_cmd(base_cmd + ["pdf"])


def _try_build_searchable_pdf_with_ocrmypdf(
    pdf_path: Path,
    output_pdf: Path,
    lang: str,
    psm: int,
    jobs: int,
) -> bool:
    cmd = [
        sys.executable,
        "-m",
        "ocrmypdf",
        "--force-ocr",
        "--optimize",
        "0",
        "--language",
        lang,
        "--tesseract-pagesegmode",
        str(psm),
        "--jobs",
        str(max(1, jobs)),
        str(pdf_path),
        str(output_pdf),
    ]
    try:
        subprocess.run(cmd, check=True)
        return True
    except (FileNotFoundError, subprocess.CalledProcessError):
        return False


class TesseractOCRAdapter(OCRAdapter):
    name = "tesseract"

    def run(self, pdf_path: Path, out_dir: Path, profile: Profile) -> OCRRunOutput:
        out_dir.mkdir(parents=True, exist_ok=True)
        image_dir = out_dir / "images"
        raw_dir = out_dir / "raw"
        raw_dir.mkdir(parents=True, exist_ok=True)

        pages = rasterize_pdf_to_png(pdf_path, image_dir=image_dir, dpi=profile.dpi)

        def _process(
            index_and_path: tuple[int, Path],
        ) -> tuple[int, OCRPage, Path, Path]:
            index, image_path = index_and_path
            page_num = index + 1
            output_base = raw_dir / f"page-{page_num:04d}"
            _run_tesseract_page(
                image_path=image_path,
                output_base=output_base,
                lang=profile.tesseract_lang,
                psm=profile.tesseract_psm,
            )

            width, height = _png_size(image_path)
            blocks = _parse_tsv(output_base.with_suffix(".tsv"))
            page = OCRPage(page=page_num, width=width, height=height, blocks=blocks)
            return (
                page_num,
                page,
                output_base.with_suffix(".pdf"),
                output_base.with_suffix(".txt"),
            )

        page_jobs = list(enumerate(pages))
        max_workers = max(1, profile.max_workers)
        if max_workers == 1:
            results = [_process(job) for job in page_jobs]
        else:
            with ThreadPoolExecutor(max_workers=max_workers) as executor:
                results = list(executor.map(_process, page_jobs))

        sorted_results = sorted(results, key=lambda item: item[0])
        ocr_pages = [item[1] for item in sorted_results]
        pdf_parts = [item[2] for item in sorted_results]
        txt_parts = [item[3] for item in sorted_results]

        searchable_pdf = out_dir / "searchable.pdf"
        used_ocrmypdf = _try_build_searchable_pdf_with_ocrmypdf(
            pdf_path=pdf_path,
            output_pdf=searchable_pdf,
            lang=profile.tesseract_lang,
            psm=profile.tesseract_psm,
            jobs=profile.max_workers,
        )

        if not used_ocrmypdf:
            if len(pdf_parts) == 1:
                shutil.copyfile(pdf_parts[0], searchable_pdf)
            else:
                _run_cmd(
                    [
                        "pdfunite",
                        *[str(path) for path in pdf_parts],
                        str(searchable_pdf),
                    ]
                )
        searchable_pdf_method = "ocrmypdf" if used_ocrmypdf else "tesseract-pdfunite"

        text_path = out_dir / "document.txt"
        with text_path.open("w", encoding="utf-8") as handle:
            for idx, txt_path in enumerate(txt_parts):
                handle.write(
                    txt_path.read_text(encoding="utf-8", errors="ignore").strip()
                )
                if idx < len(txt_parts) - 1:
                    handle.write("\n\n")

        document = OCRDocument(
            engine=self.name,
            source_pdf=str(pdf_path),
            pages=ocr_pages,
        )

        pages_json = out_dir / "pages.json"
        with pages_json.open("w", encoding="utf-8") as handle:
            json.dump(document.to_dict(), handle, ensure_ascii=False, indent=2)

        markdown_path = out_dir / "document.md"

        return OCRRunOutput(
            document=document,
            searchable_pdf=searchable_pdf,
            searchable_pdf_method=searchable_pdf_method,
            pages_json=pages_json,
            text_path=text_path,
            markdown_path=markdown_path,
        )
