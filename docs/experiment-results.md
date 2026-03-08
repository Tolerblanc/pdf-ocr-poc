# OCR Experiment Results (No Winner Fixed Yet)

Last updated: 2026-03-08

## Scope

- Fixture: `__fixtures__/fixture.pdf` (21 pages)
- Platform: Apple Silicon macOS (arm64)
- Baseline: `Tesseract + OCRmyPDF`
- Candidate set (current):
  - `paddle` (`PP-OCRv5 mobile det/rec` in this run)
  - `apple-vision` (Apple Vision via PyObjC)

## Run Summary

| Run ID | Engine | Profile | Seconds | Pages/min | KR CER | KR/EN CER | Code Acc | Layout F1 | Reading Order Error |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| `baseline-tesseract-ocrmypdf` | tesseract | fast | 44.495 | 28.318 | 0.4807 | 0.6991 | 0.1667 | 0.7278 | 0.2500 |
| `candidate-paddle-fast-v3` | paddle | fast | 219.151 | 5.749 | 0.3468 | 0.5288 | 1.0000 | 0.8020 | 0.0000 |
| `candidate-apple-vision-fast-v2` | apple-vision | fast | 34.328 | 36.705 | 0.0605 | 0.5191 | 1.0000 | 0.8833 | 0.0000 |
| `candidate-paddle-quality-v1` | paddle | quality | 302.302 | 4.168 | 0.3769 | 0.7088 | 1.0000 | 0.8020 | 0.0000 |

Source artifacts:

- `benchmarks/baseline-tesseract-ocrmypdf/run_report.json`
- `benchmarks/baseline-tesseract-ocrmypdf/eval.json`
- `benchmarks/candidate-paddle-fast-v3/run_report.json`
- `benchmarks/candidate-paddle-fast-v3/eval.json`
- `benchmarks/candidate-apple-vision-fast-v2/run_report.json`
- `benchmarks/candidate-apple-vision-fast-v2/eval.json`
- `benchmarks/candidate-paddle-quality-v1/run_report.json`
- `benchmarks/candidate-paddle-quality-v1/eval.json`

## Composite Score Comparison

- Baseline vs Paddle fast:
  - `benchmarks/comparison-fast-baseline-vs-paddle-fast-v3.json`
  - improvement ratio: `+32.81%`
- Baseline vs Apple Vision fast:
  - `benchmarks/comparison-fast-baseline-vs-apple-vision-fast-v2.json`
  - improvement ratio: `+80.58%`

## Large Fixture Performance (328 pages)

Fixture: `__fixtures__/fixture_full.pdf`

| Run ID | Engine | Max Workers | Mode | Seconds | Pages/min | OCR Stage(s) | Searchable PDF Stage(s) |
|---|---|---:|---|---:|---:|---:|---:|
| `perf-full-apple-vision-w1` | apple-vision | 1 | manual | 904.768 | 21.751 | 178.662 | 620.064 |
| `perf-full-apple-vision-w8-v2` | apple-vision | 8 | manual | 430.520 | 45.712 | 165.155 | 159.971 |
| `perf-full-apple-vision-auto-v1` | apple-vision | 8 | auto | 424.811 | 46.326 | 168.487 | 149.382 |

Speedup snapshot:

- End-to-end runtime: `2.10x` faster (`904.768s -> 430.520s`)
- Throughput: `2.10x` higher (`21.751 -> 45.712 pages/min`)
- Searchable-PDF generation stage improved the most (`620.064s -> 159.971s`) because OCRmyPDF parallelized with `--jobs=8`.
- Auto mode selected `8` workers on this machine and matched/exceeded manual `8` performance.

Source artifacts:

- `benchmarks/perf-full-apple-vision-w1/run_report.json`
- `benchmarks/perf-full-apple-vision-w1/local_only_report.json`
- `benchmarks/perf-full-apple-vision-w8-v2/run_report.json`
- `benchmarks/perf-full-apple-vision-w8-v2/local_only_report.json`
- `benchmarks/perf-full-apple-vision-auto-v1/run_report.json`
- `benchmarks/perf-full-apple-vision-auto-v1/local_only_report.json`

## V2 Vision Swift max_workers benchmark (328 pages)

Fixture: `__fixtures__/fixture_full.pdf`

Artifacts root: `artifacts/bench-worker-sweep-auto2-20260308`

| Label | Engine | Max Workers | Mode | Seconds | Pages/min | OCR Stage(s) | Searchable PDF Stage(s) | Speedup vs w1 |
|---|---|---:|---|---:|---:|---:|---:|---:|
| `w1` | vision-swift | 1 | manual | 82.862 | 237.502 | 74.776 | 7.544 | 1.00x |
| `w2` | vision-swift | 2 | manual | 78.775 | 249.826 | 71.112 | 7.517 | 1.05x |
| `w8` | vision-swift | 8 | manual | 79.095 | 248.816 | 71.340 | 7.612 | 1.05x |
| `auto` | vision-swift | 2 | auto | 78.575 | 250.463 | 70.978 | 7.453 | 1.05x |

Observations:

- `w2`, `w8`, and `auto` are effectively tied; the extra `w8` worker budget does not produce additional OCR throughput on this fixture.
- `auto` now resolves to `2` for `vision-swift`, and it lands on the fastest observed configuration in this rerun.
- `ocr_stage_profile.json` shows the saturation point directly: `w8` still reports `vision_ocr_max_active_recognize_workers=2`, `vision_ocr_max_active_render_workers=2`, and emits `vision_recognize_workers_capped=2`.
- The OCR stage still dominates total runtime (`~71s` of `~79s`), while searchable PDF generation remains a small tail (`~7.5s`).
- The benchmark harness is reproducible via `make bench-max-workers VALUES=1,2,8,auto`.

## V2 Vision Swift process-shard benchmark (328 pages)

Fixture: `__fixtures__/fixture_full.pdf`

Artifacts root: `artifacts/bench-process-shards-20260308`

| Label | Shards | Workers/Shard | Mode | Seconds | Pages/min | OCR Total(s) | Slowest Shard(s) | Speedup vs s1-w1 |
|---|---:|---:|---|---:|---:|---:|---:|---:|
| `s1-w1` | 1 | 1 | manual | 84.553 | 232.753 | 76.678 | 84.497 | 1.00x |
| `s2-w1` | 2 | 1 | manual | 49.259 | 399.523 | 89.168 | 49.209 | 1.72x |
| `s4-w1` | 4 | 1 | manual | 35.028 | 561.829 | 126.212 | 34.976 | 2.41x |
| `s8-w1` | 8 | 1 | manual | 37.584 | 523.621 | 268.293 | 37.509 | 2.25x |

Observations:

- Splitting the PDF into separate provider processes scales materially better than pushing a single process from `w2` to `w8`.
- `s4-w1` is the best result in this sweep, reaching `35.028s` (`2.41x` faster than `s1-w1`, `2.26x` faster than in-process `w8`).
- `s8-w1` regresses slightly versus `s4-w1`, which suggests process-level contention starts showing up beyond four shards on this machine.
- Each shard stays internally serial (`vision_ocr_max_active_recognize_workers=1` per shard), so the speedup comes from process-level parallelism rather than additional in-process Vision workers.
- The shard harness is reproducible via `make bench-process-shards SHARDS=1,2,4,8 MAX_WORKERS_PER_SHARD=1`.

Searchable PDF validation on `w8` output:

- Command: `make validate-searchable SEARCHABLE=./artifacts/bench-max-workers-20260308/w8/searchable.pdf PAGES=./artifacts/bench-max-workers-20260308/w8/pages.json OUT=./artifacts/bench-max-workers-20260308/w8/searchable_validation.json`
- Result: page count matched (`328/328`) and non-blank coverage passed (`308/308`), but line-match validation failed on pages `1, 93, 173, 213, 237`.
- Aggregate metrics: average line match ratio `0.9777`, minimum line match ratio `0.4667`.

## PRD Gate Check Snapshot

Current PRD gates (selected):

- AC-03 CER thresholds: KR <= 0.40, KR/EN <= 0.60
- AC-04 Code accuracy >= 0.85
- AC-05 Layout F1 >= 0.80 and reading-order error <= 0.10

Candidate status:

- `paddle-fast-v3`:
  - AC-03: PASS
  - AC-04: PASS
  - AC-05: PASS
- `apple-vision-fast-v2`:
  - AC-03: PASS
  - AC-04: PASS
  - AC-05: PASS

## Tuning Notes (Apple Vision)

- Added OCR text normalization for chapter suffix and REST path formatting (`3강 -> 3장`, `GET users/12 -> GET /users/12`).
- Added stricter Apple Vision code-line classification to reduce paragraph lines falsely tagged as code.
- Added JSON block brace repair postprocess for partially recognized code blocks.
- Updated reading-order snippet normalization to be punctuation-insensitive while preserving token order.

## Decision

- Winner is intentionally **not fixed** in this document.
- Next step is to continue candidate hardening (especially Apple Vision block typing + reading order) and rerun the same benchmark harness.
