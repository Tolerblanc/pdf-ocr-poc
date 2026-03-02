from __future__ import annotations

import subprocess
from pathlib import Path


def _page_index(path: Path) -> int:
    # Expected format: <prefix>-<n>.png
    stem = path.stem
    if "-" not in stem:
        return 0
    try:
        return int(stem.split("-")[-1])
    except ValueError:
        return 0


def rasterize_pdf_to_png(
    pdf_path: Path,
    image_dir: Path,
    dpi: int,
    prefix: str = "page",
) -> list[Path]:
    image_dir.mkdir(parents=True, exist_ok=True)
    out_prefix = image_dir / prefix

    cmd = [
        "pdftoppm",
        "-r",
        str(dpi),
        "-png",
        str(pdf_path),
        str(out_prefix),
    ]
    subprocess.run(cmd, check=True)

    pages = sorted(image_dir.glob(f"{prefix}-*.png"), key=_page_index)
    if not pages:
        raise RuntimeError(f"No rasterized images created for {pdf_path}")
    return pages
