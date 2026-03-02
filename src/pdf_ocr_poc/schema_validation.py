from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import jsonschema


def load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def default_schema_path(repo_root: Path) -> Path:
    return repo_root / "schema" / "ocr-page-v1.json"


def validate_page_payload(payload: dict[str, Any], schema_path: Path) -> None:
    schema = load_json(schema_path)
    jsonschema.validate(instance=payload, schema=schema)
