from __future__ import annotations

from dataclasses import asdict, dataclass
from typing import Any


@dataclass(slots=True)
class BBox:
    x0: float
    y0: float
    x1: float
    y1: float

    def to_dict(self) -> dict[str, float]:
        return asdict(self)


@dataclass(slots=True)
class OCRBlock:
    text: str
    bbox: BBox
    block_type: str
    confidence: float
    reading_order: int

    def to_dict(self) -> dict[str, Any]:
        data = asdict(self)
        data["bbox"] = self.bbox.to_dict()
        return data


@dataclass(slots=True)
class OCRPage:
    page: int
    width: int
    height: int
    blocks: list[OCRBlock]

    @property
    def text(self) -> str:
        ordered = sorted(self.blocks, key=lambda b: b.reading_order)
        return "\n".join(block.text for block in ordered if block.text.strip())

    @property
    def is_blank(self) -> bool:
        return not self.text.strip()

    def to_dict(self) -> dict[str, Any]:
        return {
            "page": self.page,
            "width": self.width,
            "height": self.height,
            "is_blank": self.is_blank,
            "text": self.text,
            "blocks": [block.to_dict() for block in self.blocks],
        }


@dataclass(slots=True)
class OCRDocument:
    engine: str
    source_pdf: str
    pages: list[OCRPage]

    @property
    def text(self) -> str:
        return "\n\n".join(page.text for page in self.pages)

    def to_dict(self) -> dict[str, Any]:
        return {
            "engine": self.engine,
            "source_pdf": self.source_pdf,
            "page_count": len(self.pages),
            "text": self.text,
            "pages": [page.to_dict() for page in self.pages],
        }
