from __future__ import annotations

import argparse
import json
from pathlib import Path

from .batch_runner import run_batch
from .evaluation import evaluate_against_gold
from .metrics import composite_score
from .network_guard import selfcheck_network_guard
from .pipeline import run_pipeline
from .reporting import generate_prd_ab_report, parse_candidate_arg


def _load_json(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def _effective_cer(summary: dict) -> float:
    values = []
    if summary.get("kr_prose_cer") is not None:
        values.append(float(summary["kr_prose_cer"]))
    if summary.get("mixed_prose_cer") is not None:
        values.append(float(summary["mixed_prose_cer"]))
    if not values:
        return 1.0
    return sum(values) / len(values)


def cmd_run(args: argparse.Namespace) -> int:
    output = run_pipeline(
        pdf_path=Path(args.input_pdf),
        engine=args.engine,
        profile_name_or_path=args.profile,
        out_dir=Path(args.out),
        local_only=True,
        max_workers_override=args.max_workers,
    )
    print(f"OCR completed with engine={output.document.engine}")
    print(f"searchable_pdf={output.searchable_pdf}")
    print(f"searchable_pdf_method={output.searchable_pdf_method}")
    print(f"pages_json={output.pages_json}")
    print(f"text_path={output.text_path}")
    print(f"markdown_path={output.markdown_path}")
    return 0


def cmd_batch(args: argparse.Namespace) -> int:
    result = run_batch(
        input_path=Path(args.input_path),
        output_root=Path(args.out),
        engine=args.engine,
        profile=args.profile,
        resume=args.resume,
        recursive=args.recursive,
        fail_fast=args.fail_fast,
        workers=args.workers,
        max_workers_override=args.max_workers,
    )
    print(
        f"Batch completed: total={result.total} succeeded={result.succeeded} failed={result.failed} skipped={result.skipped}"
    )
    print(f"batch_report={result.report_path}")
    return 0 if result.failed == 0 else 1


def cmd_eval(args: argparse.Namespace) -> int:
    result = evaluate_against_gold(
        gold_path=Path(args.gold),
        prediction_path=Path(args.pred),
    )
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as handle:
        json.dump(result, handle, ensure_ascii=False, indent=2)
    print(f"Evaluation saved to {out_path}")
    return 0


def cmd_compare(args: argparse.Namespace) -> int:
    baseline_eval = _load_json(Path(args.baseline_eval))
    candidate_eval = _load_json(Path(args.candidate_eval))
    baseline_report = _load_json(Path(args.baseline_report))
    candidate_report = _load_json(Path(args.candidate_report))

    baseline_summary = baseline_eval.get("summary", {})
    candidate_summary = candidate_eval.get("summary", {})

    baseline_cer = _effective_cer(baseline_summary)
    candidate_cer = _effective_cer(candidate_summary)

    baseline_code = float(baseline_summary.get("code_line_accuracy") or 0.0)
    candidate_code = float(candidate_summary.get("code_line_accuracy") or 0.0)
    baseline_layout = float(baseline_summary.get("layout_macro_f1") or 0.0)
    candidate_layout = float(candidate_summary.get("layout_macro_f1") or 0.0)

    baseline_speed = float(baseline_report.get("pages_per_minute") or 0.0)
    candidate_speed = float(candidate_report.get("pages_per_minute") or 0.0)

    baseline_composite = composite_score(
        cer_score=baseline_cer,
        code_acc=baseline_code,
        layout_f1=baseline_layout,
        pages_per_minute=baseline_speed,
        baseline_pages_per_minute=baseline_speed,
    )
    candidate_composite = composite_score(
        cer_score=candidate_cer,
        code_acc=candidate_code,
        layout_f1=candidate_layout,
        pages_per_minute=candidate_speed,
        baseline_pages_per_minute=baseline_speed,
    )

    improvement_ratio = 0.0
    if baseline_composite > 0:
        improvement_ratio = (
            candidate_composite - baseline_composite
        ) / baseline_composite

    payload = {
        "baseline_composite": baseline_composite,
        "candidate_composite": candidate_composite,
        "improvement_ratio": improvement_ratio,
        "pass_threshold": 0.10,
        "passes": improvement_ratio >= 0.10,
    }

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2)

    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


def cmd_selfcheck_local_only(_: argparse.Namespace) -> int:
    ok, message = selfcheck_network_guard()
    print(message)
    return 0 if ok else 1


def cmd_ab_report(args: argparse.Namespace) -> int:
    repo_root = Path(__file__).resolve().parents[2]
    baseline_dir = (repo_root / args.baseline_dir).resolve()
    candidates = [parse_candidate_arg(item, repo_root) for item in args.candidate]
    output = generate_prd_ab_report(
        repo_root=repo_root,
        baseline_dir=baseline_dir,
        candidates=candidates,
        output_path=(repo_root / args.out).resolve(),
    )
    print(f"ab_report={output}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="ocrpoc")
    sub = parser.add_subparsers(dest="command", required=True)

    run_parser = sub.add_parser("run", help="run OCR pipeline")
    run_parser.add_argument("input_pdf")
    run_parser.add_argument(
        "--engine",
        default="apple-vision",
        choices=["tesseract", "paddle", "apple-vision"],
    )
    run_parser.add_argument("--profile", default="fast")
    run_parser.add_argument(
        "--max-workers",
        type=int,
        default=None,
        help="optional manual override for page-level OCR workers (default: auto)",
    )
    run_parser.add_argument("--out", required=True)
    run_parser.set_defaults(func=cmd_run)

    batch_parser = sub.add_parser("batch", help="run OCR for file/folder inputs")
    batch_parser.add_argument("input_path")
    batch_parser.add_argument("--out", required=True)
    batch_parser.add_argument(
        "--engine",
        default="apple-vision",
        choices=["tesseract", "paddle", "apple-vision"],
    )
    batch_parser.add_argument("--profile", default="fast")
    batch_parser.add_argument(
        "--workers",
        type=int,
        default=1,
        help="number of PDF jobs to run concurrently",
    )
    batch_parser.add_argument(
        "--max-workers",
        type=int,
        default=None,
        help="optional manual override for page-level OCR workers per job (default: auto)",
    )
    batch_parser.add_argument("--resume", action="store_true")
    batch_parser.add_argument("--recursive", action="store_true")
    batch_parser.add_argument("--fail-fast", action="store_true")
    batch_parser.set_defaults(func=cmd_batch)

    eval_parser = sub.add_parser("eval", help="evaluate predictions against gold")
    eval_parser.add_argument("--gold", required=True)
    eval_parser.add_argument("--pred", required=True)
    eval_parser.add_argument("--out", required=True)
    eval_parser.set_defaults(func=cmd_eval)

    compare_parser = sub.add_parser(
        "compare", help="compare baseline and candidate scores"
    )
    compare_parser.add_argument("--baseline-eval", required=True)
    compare_parser.add_argument("--candidate-eval", required=True)
    compare_parser.add_argument("--baseline-report", required=True)
    compare_parser.add_argument("--candidate-report", required=True)
    compare_parser.add_argument("--out", required=True)
    compare_parser.set_defaults(func=cmd_compare)

    local_only_parser = sub.add_parser(
        "selfcheck-local-only", help="verify local-only guard"
    )
    local_only_parser.set_defaults(func=cmd_selfcheck_local_only)

    ab_parser = sub.add_parser(
        "ab-report", help="generate PRD-style A/B report from benchmark artifacts"
    )
    ab_parser.add_argument(
        "--baseline-dir",
        default="benchmarks/baseline-tesseract-ocrmypdf",
    )
    ab_parser.add_argument(
        "--candidate",
        action="append",
        default=[
            "paddle-fast-v3=benchmarks/candidate-paddle-fast-v3",
            "apple-vision-fast-v2=benchmarks/candidate-apple-vision-fast-v2",
        ],
    )
    ab_parser.add_argument("--out", default="docs/prd-ab-report.md")
    ab_parser.set_defaults(func=cmd_ab_report)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())
