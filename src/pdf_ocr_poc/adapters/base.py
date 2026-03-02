from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path

from ..models import OCRDocument
from ..profiles import Profile


@dataclass(slots=True)
class OCRRunOutput:
    document: OCRDocument
    searchable_pdf: Path
    searchable_pdf_method: str
    pages_json: Path
    text_path: Path
    markdown_path: Path
    stage_timings: dict[str, float] = field(default_factory=dict)


class OCRAdapter(ABC):
    name: str

    @abstractmethod
    def run(self, pdf_path: Path, out_dir: Path, profile: Profile) -> OCRRunOutput:
        raise NotImplementedError
