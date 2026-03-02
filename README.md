# pdf-ocr-poc (v2)

Local-only OCR CLI for scanned Korean technical PDFs on Apple Silicon.

This repository is now v2-focused:

- Go orchestrator CLI (`v2/`)
- Swift Apple Vision provider (`v2/providers/vision-swift`)
- Existing benchmark artifacts/docs (`benchmarks/`, `docs/experiment-results.md`, `docs/prd-ab-report.md`)

## Build

```bash
cd v2
go build -o bin/ocrpoc-go ./cmd/ocrpoc-go
```

Or use the root Makefile:

```bash
make doctor
make build
make test
make smoke
```

Release packaging / Homebrew draft formula:

```bash
make package
make brew-formula URL=https://github.com/Tolerblanc/pdf-ocr-poc/releases/download/v0.1.0/ocrpoc-go-v0.1.0-darwin-arm64.tar.gz
```

## Core commands

```bash
# Local-only prerequisites check
./bin/ocrpoc-go selfcheck-local-only

# Single run (mock provider)
./bin/ocrpoc-go run \
  --input ../__fixtures__/fixture.pdf \
  --out ../artifacts/v2-run \
  --provider mock

# Batch run (continue + retry failed)
./bin/ocrpoc-go batch \
  --input ../__fixtures__ \
  --out ../artifacts/v2-batch \
  --provider mock \
  --workers 2 \
  --retry-failed 1 \
  --resume

# Eval
./bin/ocrpoc-go eval \
  --gold ../fixtures/gold/v1/gold-pages.json \
  --pred ../artifacts/v2-run/pages.json \
  --out ../artifacts/v2-run/eval.json
```

## Vision provider

- Provider name: `vision-swift`
- Binary resolution order:
  1. `--provider-bin`
  2. `OCRPOC_VISION_PROVIDER_BIN`
  3. default bundled path under `v2/providers/vision-swift/bin/vision-provider`

Build/diagnostics:

```bash
cd v2/providers/vision-swift
./doctor.sh
./build.sh
```

Note: current environment may fail Swift build if CommandLineTools SDK and Swift toolchain versions are mismatched.

## Validation

```bash
cd v2
go test ./...
```

Detailed v2 status and roadmap: `docs/v2-rewrite-plan.md`.
