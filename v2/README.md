# ocrpoc-go

Go-based OCR orchestration CLI.

## Build

```bash
cd v2
go build -o bin/ocrpoc-go ./cmd/ocrpoc-go
```

From repository root, you can use:

```bash
make doctor
make build
make test
make smoke
```

Packaging / Homebrew helper:

```bash
make package
make brew-formula URL=<release-archive-url>
```

## Run (mock provider)

```bash
./bin/ocrpoc-go run \
  --input ../__fixtures__/fixture.pdf \
  --out ../artifacts/v2-run \
  --provider mock
```

## Batch (continue + retry failed)

```bash
./bin/ocrpoc-go batch \
  --input ../__fixtures__ \
  --out ../artifacts/v2-batch \
  --provider mock \
  --workers 2 \
  --retry-failed 1 \
  --resume
```

`batch` now prints a tqdm-style progress line to stderr with per-PDF totals plus current per-page OCR progress for the active PDF(s).

## Evaluate

```bash
./bin/ocrpoc-go eval \
  --gold ../fixtures/gold/v1/gold-pages.json \
  --pred ../artifacts/v2-run/pages.json \
  --out ../artifacts/v2-run/eval.json
```

## Local-only selfcheck

```bash
./bin/ocrpoc-go selfcheck-local-only
```

## Provider mode

- `--provider mock`: built-in stub provider for integration and state-flow testing.
- `--provider exec --provider-bin <path>`: external provider over stdin/stdout JSON.
- `--provider vision-swift`: Apple Vision provider wrapper; resolves binary from default locations or `OCRPOC_VISION_PROVIDER_BIN`.
- Contract schema: `v2/provider-contract.schema.json`.
- Swift provider skeleton: `v2/providers/vision-swift`.

Vision provider notes:

- Current implementation performs OCR and writes all contract artifacts.
- `max_workers` controls page-level OCR parallelism in the Swift provider.
- `ocrpoc-go batch` shows live per-page OCR progress from the provider while the outer batch bar tracks PDF completion.
- Build can fail if local Swift toolchain and SDK are mismatched.
- Use `v2/providers/vision-swift/doctor.sh` to diagnose Swift toolchain issues.
- `doctor.sh`/`build.sh` can auto-select a compatible fallback SDK; override with `SWIFT_SDK_PATH` if needed.

## Local-only enforcement (v2)

- For `exec` providers, v2 monitors provider process tree network activity using `lsof` + `pgrep`.
- `local_only_report.json` includes `selfcheck_ok`, `monitor_samples`, and `remote_connection_violations`.
- If local-only mode is enabled and remote violations are detected, command exits non-zero.

## Outputs

- `run_report.json`
- `local_only_report.json`
- `batch_state.json`
- `batch_report.json`

## Worker behavior

- `--max-workers` omitted => auto mode (`cpu-1`, capped to 8)
- `--max-workers` set => manual mode
