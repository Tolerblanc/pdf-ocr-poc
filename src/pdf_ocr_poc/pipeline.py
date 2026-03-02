from __future__ import annotations

import json
import os
import platform
import time
from dataclasses import replace
from pathlib import Path
from typing import Any

from .adapters import AppleVisionOCRAdapter, PaddleOCRAdapter, TesseractOCRAdapter
from .adapters.base import OCRAdapter, OCRRunOutput
from .network_guard import local_only_network_guard, selfcheck_network_guard
from .network_monitor import monitor_process_tree_network
from .profiles import load_profile, resolve_profile_path
from .schema_validation import default_schema_path, validate_page_payload


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _adapter_for(engine: str) -> OCRAdapter:
    lowered = engine.lower()
    if lowered == "tesseract":
        return TesseractOCRAdapter()
    if lowered == "paddle":
        return PaddleOCRAdapter()
    if lowered in {"apple-vision", "vision"}:
        return AppleVisionOCRAdapter()
    raise ValueError(f"Unsupported engine: {engine}")


def _hardware_manifest() -> dict[str, Any]:
    return {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "processor": platform.processor(),
        "python": platform.python_version(),
        "mac_ver": platform.mac_ver()[0] if platform.system() == "Darwin" else "",
    }


def _assert_platform_supported() -> None:
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise RuntimeError("This POC supports only macOS arm64 (Apple Silicon).")


def _count_pdf_pages(pdf_path: Path) -> int | None:
    try:
        import pypdfium2 as pdfium  # type: ignore
    except ImportError:
        return None

    try:
        document = pdfium.PdfDocument(str(pdf_path))
        count = int(len(document))
        document.close()
        return count
    except Exception:  # noqa: BLE001
        return None


def _recommend_auto_max_workers(engine: str, pdf_path: Path) -> int:
    cpu_count = max(1, int(os.cpu_count() or 1))
    cpu_budget = max(1, cpu_count - 1)
    page_count = _count_pdf_pages(pdf_path)

    lowered = engine.lower()
    if lowered in {"apple-vision", "vision"}:
        if page_count is None:
            target = min(6, cpu_budget)
        elif page_count < 20:
            target = min(4, cpu_budget)
        elif page_count < 80:
            target = min(6, cpu_budget)
        else:
            target = min(8, cpu_budget)
    else:
        if page_count is None:
            target = min(4, cpu_budget)
        elif page_count < 20:
            target = min(2, cpu_budget)
        elif page_count < 80:
            target = min(4, cpu_budget)
        else:
            target = min(6, cpu_budget)

    if page_count is not None:
        target = min(target, max(1, int(page_count)))
    return max(1, target)


def _render_markdown(output: OCRRunOutput) -> None:
    lines: list[str] = []
    for page in output.document.pages:
        lines.append(f"## Page {page.page}")
        for block in sorted(page.blocks, key=lambda item: item.reading_order):
            text = block.text.strip()
            if not text:
                continue
            if block.block_type == "heading":
                lines.append(f"### {text}")
            elif block.block_type == "code":
                lines.append("```text")
                lines.append(text)
                lines.append("```")
            elif block.block_type == "caption":
                lines.append(f"*{text}*")
            else:
                lines.append(text)
        lines.append("")

    output.markdown_path.write_text("\n".join(lines).strip() + "\n", encoding="utf-8")


def run_pipeline(
    pdf_path: Path,
    engine: str,
    profile_name_or_path: str,
    out_dir: Path,
    local_only: bool = True,
    max_workers_override: int | None = None,
) -> OCRRunOutput:
    _assert_platform_supported()

    repo_root = _repo_root()
    profile_path = resolve_profile_path(profile_name_or_path, repo_root)
    profile = load_profile(profile_path)
    max_workers_mode = "auto"
    if max_workers_override is not None:
        if int(max_workers_override) < 1:
            raise ValueError("max_workers_override must be >= 1")
        profile = replace(profile, max_workers=int(max_workers_override))
        max_workers_mode = "manual"
    else:
        profile = replace(
            profile,
            max_workers=_recommend_auto_max_workers(engine=engine, pdf_path=pdf_path),
        )

    adapter = _adapter_for(engine)
    out_dir.mkdir(parents=True, exist_ok=True)

    start = time.perf_counter()
    with monitor_process_tree_network() as monitor_report:
        with local_only_network_guard(enabled=local_only):
            output = adapter.run(pdf_path=pdf_path, out_dir=out_dir, profile=profile)
    elapsed = time.perf_counter() - start

    schema_path = default_schema_path(repo_root)
    for page in output.document.to_dict()["pages"]:
        validate_page_payload(page, schema_path=schema_path)

    _render_markdown(output)

    local_only_check_ok, local_only_check_message = selfcheck_network_guard()
    has_remote_violations = bool(monitor_report["violations"])
    local_only_report = {
        "local_only_mode": local_only,
        "selfcheck_ok": local_only_check_ok,
        "selfcheck_message": local_only_check_message,
        "monitor_samples": monitor_report["samples"],
        "monitor_duration_seconds": monitor_report["duration_seconds"],
        "remote_connection_violations": monitor_report["violations"],
        "monitor_ok": not has_remote_violations,
    }
    local_only_report_path = out_dir / "local_only_report.json"
    with local_only_report_path.open("w", encoding="utf-8") as handle:
        json.dump(local_only_report, handle, ensure_ascii=False, indent=2)

    if local_only and has_remote_violations:
        raise RuntimeError(
            "Local-only violation detected: subprocess/process opened remote network connections"
        )

    report = {
        "engine": adapter.name,
        "input_pdf": str(pdf_path),
        "profile": profile.name,
        "profile_path": str(profile_path),
        "effective_max_workers": profile.max_workers,
        "max_workers_mode": max_workers_mode,
        "local_only": local_only,
        "elapsed_seconds": elapsed,
        "pages": len(output.document.pages),
        "pages_per_minute": (len(output.document.pages) / elapsed) * 60
        if elapsed > 0
        else 0,
        "searchable_pdf": str(output.searchable_pdf),
        "searchable_pdf_method": output.searchable_pdf_method,
        "pages_json": str(output.pages_json),
        "text_path": str(output.text_path),
        "markdown_path": str(output.markdown_path),
        "local_only_report": str(local_only_report_path),
        "stage_timings": output.stage_timings,
        "hardware": _hardware_manifest(),
    }
    report_path = out_dir / "run_report.json"
    with report_path.open("w", encoding="utf-8") as handle:
        json.dump(report, handle, ensure_ascii=False, indent=2)

    return output
