# pdf-ocr-poc

Local-only OCR CLI for scanned Korean technical PDFs on Apple Silicon.

Korean README: `README.ko.md`

## Quick Start (Full Build + First Run)

Requirements:

- macOS on Apple Silicon (`arm64`)
- Go 1.25+
- Command Line Tools (or Xcode) with Swift

From repository root:

```bash
# 1) Validate Go/Swift environment
make doctor

# 2) Build CLI + Vision provider
make build-all

# 3) Optional: verify local-only monitor prerequisites
./v2/bin/ocrpoc-go selfcheck-local-only

# 4) Run OCR with Apple Vision provider
./v2/bin/ocrpoc-go run \
  --input ./__fixtures__/fixture.pdf \
  --out ./artifacts/v2-vision-run \
  --provider vision-swift
```

Main outputs in `--out`:

- `searchable.pdf`
- `pages.json`
- `document.txt`
- `document.md`
- `run_report.json`
- `local_only_report.json`

## Feature Overview

- Local-only execution guard with process-tree network monitoring (`lsof` + `pgrep`)
- Provider-based architecture (`vision-swift`, `exec`, `mock`)
- Single-file OCR run and batch processing
- Batch resume/retry workflow (`continue + retry failed at end`)
- Evaluation command against gold pages JSON
- Packaging helpers for release archive and Homebrew formula draft

## Common Commands

```bash
# Build only Go CLI
make build

# Run tests
make test

# Smoke tests (run + batch + eval with mock provider)
make smoke

# Benchmark fixture_full.pdf across max_workers values
make bench-max-workers VALUES=1,2,4,8

# Benchmark separate-process PDF shards
make bench-process-shards SHARDS=1,2,4,8 MAX_WORKERS_PER_SHARD=1

# Batch OCR
./v2/bin/ocrpoc-go batch \
  --input ./__fixtures__ \
  --out ./artifacts/v2-batch \
  --provider vision-swift \
  --workers 2 \
  --retry-failed 1 \
  --resume

# Evaluate against gold
./v2/bin/ocrpoc-go eval \
  --gold ./fixtures/gold/v1/gold-pages.json \
  --pred ./artifacts/v2-vision-run/pages.json \
  --out ./artifacts/v2-vision-run/eval.json

# Validate searchable PDF extraction against pages.json
make validate-searchable \
  SEARCHABLE=./artifacts/v2-vision-run/searchable.pdf \
  PAGES=./artifacts/v2-vision-run/pages.json \
  OUT=./artifacts/v2-vision-run/searchable_validation.json
```

## Swift/SDK Troubleshooting

If `make doctor` fails on Swift:

```bash
cd v2/providers/vision-swift
./doctor.sh
./build.sh
```

The scripts automatically try a compatible fallback SDK when the default SDK is incompatible with the installed Swift compiler. You can also force one manually:

```bash
SWIFT_SDK_PATH=/Library/Developer/CommandLineTools/SDKs/MacOSX15.5.sdk make build-all
```

## Packaging

```bash
make package
make brew-formula URL=https://github.com/Tolerblanc/pdf-ocr-poc/releases/download/v0.1.0/ocrpoc-go-v0.1.0-darwin-arm64.tar.gz
```

Additional implementation notes: `v2/README.md`, `docs/v2-rewrite-plan.md`.
