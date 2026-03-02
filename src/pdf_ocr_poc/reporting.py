from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from .metrics import composite_score


@dataclass(slots=True)
class RunBundle:
    name: str
    run_dir: Path
    run_report: dict
    eval_report: dict
    local_only_report: dict
    pages_payload: dict


def _load_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def _effective_cer(summary: dict) -> float:
    values: list[float] = []
    if summary.get("kr_prose_cer") is not None:
        values.append(float(summary["kr_prose_cer"]))
    if summary.get("mixed_prose_cer") is not None:
        values.append(float(summary["mixed_prose_cer"]))
    if not values:
        return 1.0
    return sum(values) / len(values)


def _load_bundle(name: str, run_dir: Path) -> RunBundle:
    return RunBundle(
        name=name,
        run_dir=run_dir,
        run_report=_load_json(run_dir / "run_report.json"),
        eval_report=_load_json(run_dir / "eval.json"),
        local_only_report=_load_json(run_dir / "local_only_report.json"),
        pages_payload=_load_json(run_dir / "pages.json"),
    )


def _candidate_ac_checks(bundle: RunBundle) -> dict[str, bool]:
    summary = bundle.eval_report["summary"]

    def _num(key: str, default: float) -> float:
        value = summary.get(key)
        if value is None:
            return default
        return float(value)

    pages = bundle.pages_payload.get("pages", [])
    non_blank_empty_pages = [
        page.get("page")
        for page in pages
        if not page.get("is_blank", False) and not str(page.get("text", "")).strip()
    ]

    local_ok = bool(bundle.local_only_report.get("monitor_ok", False)) and not bool(
        bundle.local_only_report.get("remote_connection_violations", [])
    )

    return {
        "ac01_local_only": local_ok,
        "ac03_kr_cer": _num("kr_prose_cer", 1.0) <= 0.40,
        "ac03_mixed_cer": _num("mixed_prose_cer", 1.0) <= 0.60,
        "ac04_code": _num("code_line_accuracy", 0.0) >= 0.85,
        "ac05_layout": _num("layout_macro_f1", 0.0) >= 0.80,
        "ac05_reading_order": _num("reading_order_error_ratio", 1.0) <= 0.10,
        "ac06_non_blank_non_empty": len(non_blank_empty_pages) == 0,
        "ac08_fast_runtime": float(bundle.run_report.get("elapsed_seconds") or 0.0)
        <= 480.0,
    }


def _fmt_bool(value: bool) -> str:
    return "PASS" if value else "FAIL"


def parse_candidate_arg(raw: str, repo_root: Path) -> tuple[str, Path]:
    if "=" not in raw:
        raise ValueError(f"Invalid candidate format: {raw}. expected name=path")
    name, path = raw.split("=", 1)
    candidate_path = (repo_root / path.strip()).resolve()
    return name.strip(), candidate_path


def generate_prd_ab_report(
    *,
    repo_root: Path,
    baseline_dir: Path,
    candidates: list[tuple[str, Path]],
    output_path: Path,
) -> Path:
    baseline = _load_bundle("baseline", baseline_dir)

    baseline_summary = baseline.eval_report["summary"]
    baseline_speed = float(baseline.run_report.get("pages_per_minute") or 0.0)
    baseline_composite = composite_score(
        cer_score=_effective_cer(baseline_summary),
        code_acc=float(baseline_summary.get("code_line_accuracy") or 0.0),
        layout_f1=float(baseline_summary.get("layout_macro_f1") or 0.0),
        pages_per_minute=baseline_speed,
        baseline_pages_per_minute=baseline_speed,
    )

    candidate_bundles = [_load_bundle(name, path) for name, path in candidates]

    lines: list[str] = []
    lines.append("# PRD A/B Candidate Report")
    lines.append("")
    lines.append(f"- Generated at: {datetime.now().isoformat(timespec='seconds')}")
    lines.append("- Report type: candidate comparison (winner not fixed)")
    lines.append("- Fixture: `__fixtures__/fixture.pdf` (21 pages)")
    lines.append("")
    lines.append("## Baseline")
    lines.append("")
    lines.append(f"- Run dir: `{baseline.run_dir.relative_to(repo_root)}`")
    lines.append(f"- Engine: `{baseline.run_report.get('engine')}`")
    lines.append(
        f"- Runtime: `{baseline.run_report.get('elapsed_seconds'):.3f}s` ({baseline.run_report.get('pages_per_minute'):.3f} pages/min)"
    )
    lines.append(f"- Composite score: `{baseline_composite:.6f}`")
    lines.append("")
    lines.append("## Candidate Summary")
    lines.append("")
    lines.append(
        "| Candidate | Engine | Runtime(s) | Pages/min | KR CER | KR/EN CER | Code Acc | Layout F1 | Reading Err | Composite | Improvement vs Baseline |"
    )
    lines.append("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")

    candidate_checks: dict[str, dict[str, bool]] = {}
    for candidate in candidate_bundles:
        summary = candidate.eval_report["summary"]
        candidate_composite = composite_score(
            cer_score=_effective_cer(summary),
            code_acc=float(summary.get("code_line_accuracy") or 0.0),
            layout_f1=float(summary.get("layout_macro_f1") or 0.0),
            pages_per_minute=float(candidate.run_report.get("pages_per_minute") or 0.0),
            baseline_pages_per_minute=baseline_speed,
        )
        improvement = (
            (candidate_composite - baseline_composite) / baseline_composite
            if baseline_composite > 0
            else 0.0
        )

        lines.append(
            "| "
            + " | ".join(
                [
                    candidate.name,
                    str(candidate.run_report.get("engine")),
                    f"{float(candidate.run_report.get('elapsed_seconds') or 0.0):.3f}",
                    f"{float(candidate.run_report.get('pages_per_minute') or 0.0):.3f}",
                    f"{float(summary.get('kr_prose_cer') or 0.0):.4f}",
                    f"{float(summary.get('mixed_prose_cer') or 0.0):.4f}",
                    f"{float(summary.get('code_line_accuracy') or 0.0):.4f}",
                    f"{float(summary.get('layout_macro_f1') or 0.0):.4f}",
                    f"{float(summary.get('reading_order_error_ratio') or 0.0):.4f}",
                    f"{candidate_composite:.6f}",
                    f"{improvement * 100:.2f}%",
                ]
            )
            + " |"
        )

        checks = _candidate_ac_checks(candidate)
        checks["ac10_composite_10pct"] = improvement >= 0.10
        candidate_checks[candidate.name] = checks

    lines.append("")
    lines.append("## PRD Gate Snapshot")
    lines.append("")
    lines.append(
        "| Candidate | AC-01 Local-only | AC-03 KR CER | AC-03 KR/EN CER | AC-04 Code | AC-05 Layout | AC-05 Reading Order | AC-06 Non-Blank Text | AC-08 Fast Runtime | Composite >= +10% |"
    )
    lines.append("|---|---|---|---|---|---|---|---|---|---|")
    for candidate in candidate_bundles:
        checks = candidate_checks[candidate.name]
        lines.append(
            "| "
            + " | ".join(
                [
                    candidate.name,
                    _fmt_bool(checks["ac01_local_only"]),
                    _fmt_bool(checks["ac03_kr_cer"]),
                    _fmt_bool(checks["ac03_mixed_cer"]),
                    _fmt_bool(checks["ac04_code"]),
                    _fmt_bool(checks["ac05_layout"]),
                    _fmt_bool(checks["ac05_reading_order"]),
                    _fmt_bool(checks["ac06_non_blank_non_empty"]),
                    _fmt_bool(checks["ac08_fast_runtime"]),
                    _fmt_bool(checks["ac10_composite_10pct"]),
                ]
            )
            + " |"
        )

    lines.append("")
    lines.append("## Artifact Paths")
    lines.append("")
    lines.append(f"- Baseline run: `{baseline.run_dir.relative_to(repo_root)}`")
    for candidate in candidate_bundles:
        lines.append(
            f"- Candidate run ({candidate.name}): `{candidate.run_dir.relative_to(repo_root)}`"
        )

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return output_path
