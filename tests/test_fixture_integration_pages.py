from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from pdf_ocr_poc.pipeline import run_pipeline


SELECTED_PAGES = [1, 2, 3, 4, 5, 6, 7, 10, 11, 12]


@pytest.fixture(scope="session")
def split_fixture_pages(tmp_path_factory: pytest.TempPathFactory) -> dict[int, Path]:
    if shutil.which("pdfseparate") is None:
        pytest.skip("pdfseparate is required for fixture integration tests")

    repo_root = Path(__file__).resolve().parents[1]
    fixture_pdf = repo_root / "__fixtures__" / "fixture.pdf"
    work_dir = tmp_path_factory.mktemp("fixture-pages")

    mapping: dict[int, Path] = {}
    for page in SELECTED_PAGES:
        out_path = work_dir / f"page-{page}.pdf"
        subprocess.run(
            [
                "pdfseparate",
                "-f",
                str(page),
                "-l",
                str(page),
                str(fixture_pdf),
                str(work_dir / "page-%d.pdf"),
            ],
            check=True,
        )
        assert out_path.exists()
        mapping[page] = out_path

    return mapping


@pytest.mark.integration
@pytest.mark.parametrize("page_num", SELECTED_PAGES)
def test_fixture_page_pipeline_contract(
    split_fixture_pages: dict[int, Path],
    page_num: int,
    tmp_path: Path,
) -> None:
    input_pdf = split_fixture_pages[page_num]
    out_dir = tmp_path / f"out-{page_num}"

    run_pipeline(
        pdf_path=input_pdf,
        engine="tesseract",
        profile_name_or_path="fast",
        out_dir=out_dir,
        local_only=True,
    )

    run_report = json.loads((out_dir / "run_report.json").read_text(encoding="utf-8"))
    assert run_report["pages"] == 1
    assert run_report["engine"] == "tesseract"
    assert run_report["local_only"] is True

    local_report = json.loads(
        (out_dir / "local_only_report.json").read_text(encoding="utf-8")
    )
    assert local_report["monitor_ok"] is True
    assert local_report["remote_connection_violations"] == []

    payload = json.loads((out_dir / "pages.json").read_text(encoding="utf-8"))
    assert payload["page_count"] == 1
    assert payload["pages"][0]["page"] == 1
