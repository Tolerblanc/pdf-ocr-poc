from __future__ import annotations

import re
import unicodedata


def normalize_unicode(text: str) -> str:
    return unicodedata.normalize("NFC", text)


def _rstrip_lines(text: str) -> str:
    return "\n".join(line.rstrip() for line in text.splitlines())


def normalize_prose(text: str) -> str:
    text = normalize_unicode(text).replace("\r\n", "\n").replace("\r", "\n")
    text = _rstrip_lines(text)
    text = re.sub(r"[\t ]+", " ", text)
    return text.strip()


def normalize_code(text: str) -> str:
    text = normalize_unicode(text).replace("\r\n", "\n").replace("\r", "\n")
    text = _rstrip_lines(text)
    return text.strip("\n")


def tokenize_for_wer(text: str) -> list[str]:
    normalized = normalize_prose(text)
    if not normalized:
        return []
    return normalized.split()
