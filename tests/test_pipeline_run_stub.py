from __future__ import annotations

import json
from pathlib import Path

import pytest

from pdf_ocr_poc.adapters.base import OCRRunOutput
from pdf_ocr_poc.models import BBox, OCRBlock, OCRDocument, OCRPage
from pdf_ocr_poc.pipeline import run_pipeline


class _StubAdapter:
    name = "stub"

    def run(self, pdf_path: Path, out_dir: Path, profile) -> OCRRunOutput:  # noqa: ANN001
        page = OCRPage(
            page=1,
            width=100,
            height=200,
            blocks=[
                OCRBlock(
                    text="1장 테스트",
                    bbox=BBox(0, 0, 10, 10),
                    block_type="heading",
                    confidence=99.0,
                    reading_order=1,
                ),
                OCRBlock(
                    text="본문 테스트",
                    bbox=BBox(0, 20, 50, 40),
                    block_type="paragraph",
                    confidence=98.0,
                    reading_order=2,
                ),
            ],
        )
        document = OCRDocument(engine="stub", source_pdf=str(pdf_path), pages=[page])

        searchable_pdf = out_dir / "searchable.pdf"
        searchable_pdf.write_bytes(b"%PDF-1.4\n")

        pages_json = out_dir / "pages.json"
        pages_json.write_text(
            json.dumps(document.to_dict(), ensure_ascii=False), encoding="utf-8"
        )

        text_path = out_dir / "document.txt"
        text_path.write_text(document.text, encoding="utf-8")

        markdown_path = out_dir / "document.md"

        return OCRRunOutput(
            document=document,
            searchable_pdf=searchable_pdf,
            searchable_pdf_method="stub",
            pages_json=pages_json,
            text_path=text_path,
            markdown_path=markdown_path,
        )


def test_run_pipeline_writes_reports(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("pdf_ocr_poc.pipeline._adapter_for", lambda _: _StubAdapter())
    input_pdf = tmp_path / "in.pdf"
    input_pdf.write_bytes(b"%PDF-1.4\n")

    out_dir = tmp_path / "out"
    run_pipeline(
        pdf_path=input_pdf,
        engine="tesseract",
        profile_name_or_path="fast",
        out_dir=out_dir,
        local_only=True,
    )

    assert (out_dir / "run_report.json").exists()
    assert (out_dir / "local_only_report.json").exists()
    assert (out_dir / "document.md").exists()

    run_report = json.loads((out_dir / "run_report.json").read_text(encoding="utf-8"))
    assert run_report["searchable_pdf_method"] == "stub"

    local_report = json.loads(
        (out_dir / "local_only_report.json").read_text(encoding="utf-8")
    )
    assert local_report["local_only_mode"] is True
    assert local_report["selfcheck_ok"] is True


def test_run_pipeline_platform_guard(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.setattr("pdf_ocr_poc.pipeline._adapter_for", lambda _: _StubAdapter())
    monkeypatch.setattr("platform.system", lambda: "Linux")
    monkeypatch.setattr("platform.machine", lambda: "x86_64")

    with pytest.raises(RuntimeError):
        run_pipeline(
            pdf_path=tmp_path / "in.pdf",
            engine="tesseract",
            profile_name_or_path="fast",
            out_dir=tmp_path / "out",
            local_only=True,
        )


def test_run_pipeline_applies_max_workers_override(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    captured: dict[str, int] = {}

    class _CaptureAdapter(_StubAdapter):
        name = "capture"

        def run(self, pdf_path: Path, out_dir: Path, profile) -> OCRRunOutput:  # noqa: ANN001
            captured["max_workers"] = int(profile.max_workers)
            output = super().run(pdf_path, out_dir, profile)
            output.stage_timings = {"vision_ocr_seconds": 0.1}
            return output

    monkeypatch.setattr(
        "pdf_ocr_poc.pipeline._adapter_for", lambda _: _CaptureAdapter()
    )

    input_pdf = tmp_path / "in.pdf"
    input_pdf.write_bytes(b"%PDF-1.4\n")

    out_dir = tmp_path / "out"
    run_pipeline(
        pdf_path=input_pdf,
        engine="apple-vision",
        profile_name_or_path="fast",
        out_dir=out_dir,
        local_only=True,
        max_workers_override=7,
    )

    assert captured["max_workers"] == 7
    run_report = json.loads((out_dir / "run_report.json").read_text(encoding="utf-8"))
    assert run_report["effective_max_workers"] == 7
    assert run_report["max_workers_mode"] == "manual"
    assert run_report["stage_timings"]["vision_ocr_seconds"] == 0.1


def test_run_pipeline_rejects_invalid_workers_override(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("pdf_ocr_poc.pipeline._adapter_for", lambda _: _StubAdapter())
    input_pdf = tmp_path / "in.pdf"
    input_pdf.write_bytes(b"%PDF-1.4\n")

    with pytest.raises(ValueError):
        run_pipeline(
            pdf_path=input_pdf,
            engine="tesseract",
            profile_name_or_path="fast",
            out_dir=tmp_path / "out",
            local_only=True,
            max_workers_override=0,
        )


def test_run_pipeline_auto_workers_when_override_missing(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    captured: dict[str, int] = {}

    class _CaptureAdapter(_StubAdapter):
        name = "capture"

        def run(self, pdf_path: Path, out_dir: Path, profile) -> OCRRunOutput:  # noqa: ANN001
            captured["max_workers"] = int(profile.max_workers)
            return super().run(pdf_path, out_dir, profile)

    monkeypatch.setattr(
        "pdf_ocr_poc.pipeline._adapter_for", lambda _: _CaptureAdapter()
    )
    monkeypatch.setattr("pdf_ocr_poc.pipeline._count_pdf_pages", lambda _: 328)
    monkeypatch.setattr("os.cpu_count", lambda: 10)

    input_pdf = tmp_path / "in.pdf"
    input_pdf.write_bytes(b"%PDF-1.4\n")

    out_dir = tmp_path / "out"
    run_pipeline(
        pdf_path=input_pdf,
        engine="apple-vision",
        profile_name_or_path="fast",
        out_dir=out_dir,
        local_only=True,
    )

    assert captured["max_workers"] == 8
    run_report = json.loads((out_dir / "run_report.json").read_text(encoding="utf-8"))
    assert run_report["effective_max_workers"] == 8
    assert run_report["max_workers_mode"] == "auto"
