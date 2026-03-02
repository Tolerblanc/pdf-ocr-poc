from __future__ import annotations

from pathlib import Path

from pdf_ocr_poc.adapters.tesseract import _parse_tsv, _png_size


def test_png_size_for_invalid_file(tmp_path: Path) -> None:
    path = tmp_path / "x.bin"
    path.write_bytes(b"notpng")
    assert _png_size(path) == (0, 0)


def test_parse_tsv_groups_words_into_line(tmp_path: Path) -> None:
    tsv = tmp_path / "page.tsv"
    tsv.write_text(
        "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"
        "5\t1\t1\t1\t1\t1\t10\t20\t30\t10\t96\tHello\n"
        "5\t1\t1\t1\t1\t2\t45\t20\t20\t10\t94\tWorld\n",
        encoding="utf-8",
    )
    blocks = _parse_tsv(tsv)
    assert len(blocks) == 1
    assert blocks[0].text == "Hello World"
    assert blocks[0].reading_order == 1


def test_parse_tsv_ignores_empty_words(tmp_path: Path) -> None:
    tsv = tmp_path / "page.tsv"
    tsv.write_text(
        "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"
        "5\t1\t1\t1\t1\t1\t0\t0\t0\t0\t0\t\n",
        encoding="utf-8",
    )
    assert _parse_tsv(tsv) == []


def test_parse_tsv_sorts_by_position(tmp_path: Path) -> None:
    tsv = tmp_path / "page.tsv"
    tsv.write_text(
        "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"
        "5\t1\t1\t1\t1\t1\t0\t100\t10\t10\t90\tLater\n"
        "5\t1\t2\t1\t1\t1\t0\t10\t10\t10\t90\tFirst\n",
        encoding="utf-8",
    )
    blocks = _parse_tsv(tsv)
    assert [block.text for block in blocks] == ["First", "Later"]
