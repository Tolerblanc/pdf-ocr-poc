from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from pdf_ocr_poc.evaluation import evaluate_against_gold
from pdf_ocr_poc.pipeline import run_pipeline


@pytest.mark.integration
def test_fixture_page1_end_to_end(tmp_path: Path) -> None:
    if shutil.which("pdfseparate") is None:
        pytest.skip("pdfseparate is required for fixture integration test")

    repo_root = Path(__file__).resolve().parents[1]
    fixture_pdf = repo_root / "__fixtures__" / "fixture.pdf"

    page_dir = tmp_path / "pages"
    page_dir.mkdir(parents=True, exist_ok=True)
    page_pdf = page_dir / "page-1.pdf"
    subprocess.run(
        [
            "pdfseparate",
            "-f",
            "1",
            "-l",
            "1",
            str(fixture_pdf),
            str(page_dir / "page-%d.pdf"),
        ],
        check=True,
    )

    out_dir = tmp_path / "out"
    run_pipeline(
        pdf_path=page_pdf,
        engine="tesseract",
        profile_name_or_path="fast",
        out_dir=out_dir,
        local_only=True,
    )

    assert (out_dir / "searchable.pdf").exists()
    assert (out_dir / "pages.json").exists()
    assert (out_dir / "document.txt").exists()
    assert (out_dir / "document.md").exists()

    run_report = json.loads((out_dir / "run_report.json").read_text(encoding="utf-8"))
    assert run_report["engine"] == "tesseract"
    assert run_report["searchable_pdf_method"] in {"ocrmypdf", "tesseract-pdfunite"}

    local_only = json.loads(
        (out_dir / "local_only_report.json").read_text(encoding="utf-8")
    )
    assert local_only["local_only_mode"] is True
    assert local_only["monitor_ok"] is True

    gold = {
        "version": "v1",
        "pages": [
            {
                "page": 1,
                "prose_kr": "차례",
                "expected_block_types": ["heading", "paragraph"],
            }
        ],
    }
    gold_path = tmp_path / "gold.json"
    gold_path.write_text(json.dumps(gold, ensure_ascii=False), encoding="utf-8")

    eval_result = evaluate_against_gold(gold_path, out_dir / "pages.json")
    assert "summary" in eval_result
