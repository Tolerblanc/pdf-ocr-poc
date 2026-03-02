from __future__ import annotations

from pathlib import Path

import pytest

from pdf_ocr_poc.pdf_images import rasterize_pdf_to_png


def test_rasterize_pdf_to_png_raises_when_no_images(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    def fake_run(*args, **kwargs):  # noqa: ANN002, ANN003
        return None

    monkeypatch.setattr("subprocess.run", fake_run)
    with pytest.raises(RuntimeError):
        rasterize_pdf_to_png(tmp_path / "in.pdf", tmp_path / "images", dpi=200)


def test_rasterize_pdf_to_png_returns_sorted(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    images_dir = tmp_path / "images"

    def fake_run(*args, **kwargs):  # noqa: ANN002, ANN003
        images_dir.mkdir(parents=True, exist_ok=True)
        (images_dir / "page-10.png").write_bytes(b"x")
        (images_dir / "page-2.png").write_bytes(b"x")
        return None

    monkeypatch.setattr("subprocess.run", fake_run)
    pages = rasterize_pdf_to_png(tmp_path / "in.pdf", images_dir, dpi=200)
    assert [path.name for path in pages] == ["page-2.png", "page-10.png"]
