from __future__ import annotations

import json
import threading
import time
from pathlib import Path

import pytest

from pdf_ocr_poc.batch_runner import run_batch


def _write_pdf(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"%PDF-1.4\n")


def _stub_pipeline(
    pdf_path: Path,
    engine: str,
    profile_name_or_path: str,
    out_dir: Path,
    local_only: bool,
    **kwargs,
):  # noqa: ANN001
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "run_report.json").write_text(
        json.dumps(
            {
                "engine": engine,
                "input_pdf": str(pdf_path),
                "profile": profile_name_or_path,
                "local_only": local_only,
                "elapsed_seconds": 1.0,
                "pages": 1,
                "pages_per_minute": 60.0,
            }
        ),
        encoding="utf-8",
    )


def test_batch_runner_basic_and_resume(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    input_dir = tmp_path / "in"
    _write_pdf(input_dir / "a.pdf")
    _write_pdf(input_dir / "b.pdf")

    monkeypatch.setattr("pdf_ocr_poc.batch_runner.run_pipeline", _stub_pipeline)

    out_dir = tmp_path / "out"
    first = run_batch(
        input_path=input_dir,
        output_root=out_dir,
        engine="apple-vision",
        profile="fast",
        resume=False,
        recursive=False,
        fail_fast=False,
    )
    assert first.total == 2
    assert first.succeeded == 2
    assert first.failed == 0
    assert first.skipped == 0

    second = run_batch(
        input_path=input_dir,
        output_root=out_dir,
        engine="apple-vision",
        profile="fast",
        resume=True,
        recursive=False,
        fail_fast=False,
    )
    assert second.total == 2
    assert second.succeeded == 0
    assert second.failed == 0
    assert second.skipped == 2


def test_batch_runner_fail_fast(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    input_dir = tmp_path / "in"
    _write_pdf(input_dir / "a.pdf")
    _write_pdf(input_dir / "b.pdf")

    def failing_pipeline(
        pdf_path: Path,
        engine: str,
        profile_name_or_path: str,
        out_dir: Path,
        local_only: bool,
        **kwargs,
    ):  # noqa: ANN001
        if pdf_path.name == "a.pdf":
            raise RuntimeError("simulated failure")
        _stub_pipeline(
            pdf_path,
            engine,
            profile_name_or_path,
            out_dir,
            local_only,
            **kwargs,
        )

    monkeypatch.setattr("pdf_ocr_poc.batch_runner.run_pipeline", failing_pipeline)

    out_dir = tmp_path / "out"
    result = run_batch(
        input_path=input_dir,
        output_root=out_dir,
        engine="apple-vision",
        profile="fast",
        resume=False,
        recursive=False,
        fail_fast=True,
    )
    assert result.failed == 1
    assert result.total == 1


def test_batch_runner_parallel_workers_and_override(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    input_dir = tmp_path / "in"
    _write_pdf(input_dir / "a.pdf")
    _write_pdf(input_dir / "b.pdf")
    _write_pdf(input_dir / "c.pdf")

    seen_overrides: list[int] = []
    lock = threading.Lock()

    def parallel_stub(
        pdf_path: Path,
        engine: str,
        profile_name_or_path: str,
        out_dir: Path,
        local_only: bool,
        **kwargs,
    ):  # noqa: ANN001
        time.sleep(0.05)
        with lock:
            seen_overrides.append(int(kwargs.get("max_workers_override") or 0))
        _stub_pipeline(
            pdf_path,
            engine,
            profile_name_or_path,
            out_dir,
            local_only,
            **kwargs,
        )

    monkeypatch.setattr("pdf_ocr_poc.batch_runner.run_pipeline", parallel_stub)

    out_dir = tmp_path / "out"
    result = run_batch(
        input_path=input_dir,
        output_root=out_dir,
        engine="apple-vision",
        profile="fast",
        resume=False,
        recursive=False,
        fail_fast=False,
        workers=3,
        max_workers_override=6,
    )

    assert result.total == 3
    assert result.succeeded == 3
    assert result.failed == 0
    assert sorted(seen_overrides) == [6, 6, 6]

    report = json.loads((out_dir / "batch_report.json").read_text(encoding="utf-8"))
    assert report["workers_requested"] == 3
    assert report["effective_workers"] == 3
    assert report["max_workers_override"] == 6


def test_batch_runner_fail_fast_forces_single_worker(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    input_dir = tmp_path / "in"
    _write_pdf(input_dir / "a.pdf")
    _write_pdf(input_dir / "b.pdf")

    def fail_first(
        pdf_path: Path,
        engine: str,
        profile_name_or_path: str,
        out_dir: Path,
        local_only: bool,
        **kwargs,
    ):  # noqa: ANN001
        if pdf_path.name == "a.pdf":
            raise RuntimeError("simulated failure")
        _stub_pipeline(
            pdf_path,
            engine,
            profile_name_or_path,
            out_dir,
            local_only,
            **kwargs,
        )

    monkeypatch.setattr("pdf_ocr_poc.batch_runner.run_pipeline", fail_first)

    out_dir = tmp_path / "out"
    result = run_batch(
        input_path=input_dir,
        output_root=out_dir,
        engine="apple-vision",
        profile="fast",
        resume=False,
        recursive=False,
        fail_fast=True,
        workers=4,
    )

    assert result.failed == 1
    assert result.total == 1

    report = json.loads((out_dir / "batch_report.json").read_text(encoding="utf-8"))
    assert report["workers_requested"] == 4
    assert report["effective_workers"] == 1
