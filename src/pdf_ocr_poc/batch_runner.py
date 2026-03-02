from __future__ import annotations

import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

from .pipeline import run_pipeline


@dataclass(slots=True)
class BatchRunResult:
    total: int
    succeeded: int
    failed: int
    skipped: int
    report_path: Path


def _discover_pdfs(input_path: Path, recursive: bool) -> list[Path]:
    if input_path.is_file():
        if input_path.suffix.lower() != ".pdf":
            raise ValueError(f"Input file must be a PDF: {input_path}")
        return [input_path]

    if not input_path.is_dir():
        raise FileNotFoundError(f"Input path not found: {input_path}")

    if recursive:
        candidates = sorted(input_path.rglob("*"))
    else:
        candidates = sorted(input_path.glob("*"))
    pdfs = [
        path for path in candidates if path.is_file() and path.suffix.lower() == ".pdf"
    ]
    if not pdfs:
        raise FileNotFoundError(f"No PDF files found under: {input_path}")
    return pdfs


def _run_dir_for_pdf(pdf_path: Path, input_root: Path, output_root: Path) -> Path:
    if input_root.is_file():
        return output_root / pdf_path.stem

    relative = pdf_path.relative_to(input_root)
    return output_root / relative.with_suffix("")


def _write_job_status(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


def _run_single_job(
    *,
    pdf_path: Path,
    run_dir: Path,
    engine: str,
    profile: str,
    max_workers_override: int | None,
) -> dict:
    job_status_path = run_dir / "job_status.json"
    start = time.perf_counter()
    _write_job_status(
        job_status_path,
        {
            "input_pdf": str(pdf_path),
            "run_dir": str(run_dir),
            "status": "running",
            "engine": engine,
            "profile": profile,
            "max_workers_override": max_workers_override,
            "started_at": time.time(),
        },
    )

    run_kwargs = {}
    if max_workers_override is not None:
        run_kwargs["max_workers_override"] = int(max_workers_override)

    try:
        run_pipeline(
            pdf_path=pdf_path,
            engine=engine,
            profile_name_or_path=profile,
            out_dir=run_dir,
            local_only=True,
            **run_kwargs,
        )
        elapsed = time.perf_counter() - start
        status_payload = {
            "input_pdf": str(pdf_path),
            "run_dir": str(run_dir),
            "status": "succeeded",
            "elapsed_seconds": elapsed,
            "engine": engine,
            "profile": profile,
            "max_workers_override": max_workers_override,
            "completed_at": time.time(),
        }
        _write_job_status(job_status_path, status_payload)
        return status_payload
    except Exception as exc:  # noqa: BLE001
        elapsed = time.perf_counter() - start
        status_payload = {
            "input_pdf": str(pdf_path),
            "run_dir": str(run_dir),
            "status": "failed",
            "elapsed_seconds": elapsed,
            "engine": engine,
            "profile": profile,
            "max_workers_override": max_workers_override,
            "error": str(exc),
            "completed_at": time.time(),
        }
        _write_job_status(job_status_path, status_payload)
        return status_payload


def run_batch(
    *,
    input_path: Path,
    output_root: Path,
    engine: str,
    profile: str,
    resume: bool,
    recursive: bool,
    fail_fast: bool,
    workers: int = 1,
    max_workers_override: int | None = None,
) -> BatchRunResult:
    if workers < 1:
        raise ValueError("workers must be >= 1")
    if max_workers_override is not None and int(max_workers_override) < 1:
        raise ValueError("max_workers_override must be >= 1")

    pdfs = _discover_pdfs(input_path, recursive=recursive)
    output_root.mkdir(parents=True, exist_ok=True)

    jobs: list[dict] = []
    runnable_jobs: list[tuple[Path, Path]] = []
    succeeded = 0
    failed = 0
    skipped = 0

    for pdf_path in pdfs:
        run_dir = _run_dir_for_pdf(
            pdf_path=pdf_path, input_root=input_path, output_root=output_root
        )
        run_report_path = run_dir / "run_report.json"

        if resume and run_report_path.exists():
            skipped += 1
            jobs.append(
                {
                    "input_pdf": str(pdf_path),
                    "run_dir": str(run_dir),
                    "status": "skipped",
                    "reason": "resume-enabled and run_report.json already exists",
                }
            )
            continue

        runnable_jobs.append((pdf_path, run_dir))

    effective_workers = workers
    if fail_fast and workers > 1:
        effective_workers = 1

    if effective_workers == 1:
        for pdf_path, run_dir in runnable_jobs:
            status_payload = _run_single_job(
                pdf_path=pdf_path,
                run_dir=run_dir,
                engine=engine,
                profile=profile,
                max_workers_override=max_workers_override,
            )
            jobs.append(status_payload)
            if status_payload["status"] == "succeeded":
                succeeded += 1
            else:
                failed += 1
                if fail_fast:
                    break
    else:
        with ThreadPoolExecutor(max_workers=effective_workers) as executor:
            futures = [
                executor.submit(
                    _run_single_job,
                    pdf_path=pdf_path,
                    run_dir=run_dir,
                    engine=engine,
                    profile=profile,
                    max_workers_override=max_workers_override,
                )
                for pdf_path, run_dir in runnable_jobs
            ]
            for future in as_completed(futures):
                status_payload = future.result()
                jobs.append(status_payload)
                if status_payload["status"] == "succeeded":
                    succeeded += 1
                else:
                    failed += 1

    jobs = sorted(jobs, key=lambda item: str(item.get("input_pdf", "")))

    report = {
        "input_path": str(input_path),
        "output_root": str(output_root),
        "engine": engine,
        "profile": profile,
        "resume": resume,
        "recursive": recursive,
        "fail_fast": fail_fast,
        "workers_requested": workers,
        "effective_workers": effective_workers,
        "max_workers_override": max_workers_override,
        "total": len(jobs),
        "succeeded": succeeded,
        "failed": failed,
        "skipped": skipped,
        "jobs": jobs,
    }

    report_path = output_root / "batch_report.json"
    report_path.write_text(
        json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8"
    )

    return BatchRunResult(
        total=len(jobs),
        succeeded=succeeded,
        failed=failed,
        skipped=skipped,
        report_path=report_path,
    )
