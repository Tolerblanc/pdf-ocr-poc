# LuxShot Repository Analysis

Target repository: `https://github.com/lukebuild/LuxShot`

## What LuxShot Is

- Native macOS menu-bar OCR and capture app.
- Stack: SwiftUI + Vision framework.
- OCR engine: `VNRecognizeTextRequest` (on-device Apple Vision).

Relevant files reviewed:

- `tmp/LuxShot/README.md`
- `tmp/LuxShot/LuxShot/OCRManager.swift`
- `tmp/LuxShot/LICENSE`

## OCR Implementation Findings

From `OCRManager.swift`:

- Uses `VNRecognizeTextRequest` with:
  - `recognitionLevel = .accurate`
  - `usesLanguageCorrection = true`
  - `automaticallyDetectsLanguage = true`
  - `revision = VNRecognizeTextRequestRevision3` on macOS 14+
- Extracts top candidate text per observation and joins with line breaks.
- Runs barcode detection separately via `VNDetectBarcodesRequest`.

## Fit for This POC

How it maps to our requirements:

- Local-only execution: very strong fit (Apple on-device engine).
- Apple Silicon target: strong fit.
- KR/EN mixed technical text: promising baseline, but requires our own postprocess for block typing and reading order quality.
- PDF pipeline compatibility: LuxShot itself is screenshot-first, so direct reuse is limited; adapter layer is needed for PDF page raster workflow.

## License and Reuse Constraints

- LuxShot is GPLv3 (`tmp/LuxShot/LICENSE`).
- Direct code copying into this MIT project is not recommended.
- Safe approach: reuse ideas and public API usage patterns, but implement our own adapter code from scratch.

## Integration Feasibility Verdict

Verdict: **Feasible as an additional experiment candidate**.

Implemented approach in this repo:

- Added `apple-vision` adapter using PyObjC Vision bindings (own implementation, no LuxShot code copy):
  - `src/pdf_ocr_poc/adapters/apple_vision.py`
- Added engine wiring:
  - `src/pdf_ocr_poc/pipeline.py`
  - `src/pdf_ocr_poc/cli.py`
  - `src/pdf_ocr_poc/adapters/__init__.py`
- Added run script:
  - `scripts/run_candidate_apple_vision.sh`

## Initial POC Status

- Full fixture run completed:
  - `benchmarks/candidate-apple-vision-fast-v2/run_report.json`
  - `benchmarks/candidate-apple-vision-fast-v2/eval.json`
  - `benchmarks/candidate-apple-vision-fast-v2/local_only_report.json`
- Baseline comparison generated:
  - `benchmarks/comparison-fast-baseline-vs-apple-vision-fast-v2.json`

## Caveats Observed

- Apple Vision v2 tuning improved block typing and reading-order significantly; current fast profile passes PRD gates on the fixture subset.
- Winner is still intentionally not fixed in this analysis; final product decision is deferred to the next decision step.
