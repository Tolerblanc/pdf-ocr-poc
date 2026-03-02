# pdf-ocr-poc

Local-only OCR POC for scanned Korean technical PDFs on Apple Silicon.

## Quick start

System prerequisites (macOS):

```bash
brew install tesseract tesseract-lang ghostscript poppler
```

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e .[dev]
```

Optional engine dependencies:

```bash
# Paddle candidate
pip install -e .[paddle]

# Apple Vision candidate (PyObjC bindings)
pip install -e .[apple_vision]
```

Run OCR on the fixture:

```bash
ocrpoc run "__fixtures__/fixture.pdf" --engine apple-vision --profile fast --out "artifacts/run-fast"
```

This generates:

- `searchable.pdf` (OCRmyPDF-backed baseline searchable PDF)
- `pages.json` (schema-validated page blocks, includes `is_blank`)
- `document.txt`
- `document.md`
- `run_report.json` (timings, hardware, method)
- `local_only_report.json` (runtime network monitor evidence)

Run evaluation (after preparing/refreshing gold):

```bash
ocrpoc eval --gold "fixtures/gold/v1/gold-pages.json" --pred "artifacts/run-fast/pages.json" --out "artifacts/run-fast/eval.json"
```

Run tests:

```bash
pytest
```

Generate PRD-style A/B report from benchmark artifacts:

```bash
python scripts/generate_prd_ab_report.py --out "docs/prd-ab-report.md"
```

## Engines

- `apple-vision` (default): Apple Vision (`VNRecognizeTextRequest`) adapter via PyObjC.
- `tesseract`: baseline local OCR adapter.
- `paddle` (optional): candidate adapter if `paddleocr` is installed.

## Program flow

1. CLI (`ocrpoc run`/`ocrpoc batch`) enters `run_pipeline()`.
2. Profile is loaded from `configs/profiles/*.yaml` and optional worker override is applied.
3. OCR adapter runs, writes `searchable.pdf`, `pages.json`, `document.txt`, `document.md`.
4. Pipeline writes `run_report.json` + `local_only_report.json` with timing and local-only evidence.

Key files:

- `src/pdf_ocr_poc/cli.py`
- `src/pdf_ocr_poc/pipeline.py`
- `src/pdf_ocr_poc/adapters/apple_vision.py`
- `src/pdf_ocr_poc/batch_runner.py`

## Performance tuning (large PDFs)

Single large PDF: by default, worker count is auto-tuned from PDF page count and CPU.

```bash
ocrpoc run "__fixtures__/fixture_full.pdf" --engine apple-vision --profile fast --out "benchmarks/full-apple-vision-auto"
```

If needed, override auto selection manually:

```bash
ocrpoc run "__fixtures__/fixture_full.pdf" --engine apple-vision --profile fast --max-workers 8 --out "benchmarks/full-apple-vision-w8"
```

Batch folder processing: combine file-level and page-level parallelism.

```bash
ocrpoc batch "<pdf-folder>" --engine apple-vision --profile fast --workers 3 --max-workers 6 --out "artifacts/batch"
```

Notes:

- `--max-workers`: page-level parallelism inside one PDF run.
- `--max-workers` is optional; omitted means automatic worker selection.
- `--workers`: PDF-level parallelism in batch mode.
- `--fail-fast` forces effective batch workers to 1 for deterministic early stop.
- See `run_report.json.stage_timings` for per-stage timing breakdown.

## Notes

- Local-only mode is enabled by default during pipeline execution.
- The POC enforces `macOS arm64` runtime.
- This repository uses a fixed fixture PDF at `__fixtures__/fixture.pdf`.
