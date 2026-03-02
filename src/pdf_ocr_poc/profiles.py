from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml


@dataclass(slots=True)
class Profile:
    name: str
    dpi: int
    tesseract_lang: str
    tesseract_psm: int
    max_workers: int


def _parse_profile(data: dict[str, Any], name: str) -> Profile:
    return Profile(
        name=name,
        dpi=int(data.get("dpi", 300)),
        tesseract_lang=str(data.get("tesseract_lang", "eng+kor")),
        tesseract_psm=int(data.get("tesseract_psm", 6)),
        max_workers=int(data.get("max_workers", 4)),
    )


def load_profile(profile_path: Path) -> Profile:
    with profile_path.open("r", encoding="utf-8") as handle:
        data = yaml.safe_load(handle) or {}
    if not isinstance(data, dict):
        raise ValueError(f"Invalid profile format: {profile_path}")

    name = str(data.get("name", profile_path.stem))
    return _parse_profile(data, name)


def resolve_profile_path(profile_name_or_path: str, repo_root: Path) -> Path:
    candidate = Path(profile_name_or_path)
    if candidate.exists():
        return candidate

    mapped = repo_root / "configs" / "profiles" / f"{profile_name_or_path}.yaml"
    if not mapped.exists():
        raise FileNotFoundError(f"Profile not found: {profile_name_or_path}")
    return mapped
