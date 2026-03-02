from __future__ import annotations

import re

_HEADING_RE = re.compile(r"^(\d+\s*장|chapter\s+\d+|part\s+\d+)", re.IGNORECASE)
_CAPTION_RE = re.compile(r"^(그림|표|fig\.?|figure|table)\s*", re.IGNORECASE)
_CODE_HINT_RE = re.compile(
    r"(\bdef\b|\bclass\b|\breturn\b|\bimport\b|\bSELECT\b|\bFROM\b|\bWHERE\b|[{}();=<>&]|\bGET\s+/)",
    re.IGNORECASE,
)


def _symbol_ratio(text: str) -> float:
    symbols = sum(1 for ch in text if ch in "{}[]();=<>:+-*/_.`\"'\\")
    chars = len(text.strip())
    if chars == 0:
        return 0.0
    return symbols / chars


def classify_block(text: str) -> str:
    stripped = text.strip()
    if not stripped:
        return "paragraph"

    if _HEADING_RE.search(stripped):
        return "heading"

    if _CAPTION_RE.search(stripped):
        return "caption"

    if _CODE_HINT_RE.search(stripped) or _symbol_ratio(stripped) >= 0.12:
        return "code"

    return "paragraph"
