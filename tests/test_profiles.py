from __future__ import annotations

from pathlib import Path

import pytest

from pdf_ocr_poc.profiles import load_profile, resolve_profile_path


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def test_load_fast_profile() -> None:
    path = _repo_root() / "configs" / "profiles" / "fast.yaml"
    profile = load_profile(path)
    assert profile.name == "fast"
    assert profile.dpi == 220


def test_resolve_profile_path_by_name() -> None:
    path = resolve_profile_path("quality", _repo_root())
    assert path.name == "quality.yaml"


def test_resolve_profile_path_missing() -> None:
    with pytest.raises(FileNotFoundError):
        resolve_profile_path("not-a-profile", _repo_root())
