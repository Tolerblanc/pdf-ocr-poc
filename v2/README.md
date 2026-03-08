# ocrpoc-go

Go-based OCR orchestration CLI.

## Quickstart From Repo Root

```bash
make quickstart
```

This is the shortest clone-to-first-run path. It runs doctor checks, builds `ocrpoc-go`, builds the bundled Vision provider when needed, verifies OCR local-only prerequisites, and OCRs `__fixtures__/fixture.pdf` into `artifacts/v2-quickstart`.

Useful overrides:

```bash
make quickstart QUICKSTART_INPUT=./my.pdf QUICKSTART_OUT=./artifacts/my-run
make quickstart QUICKSTART_PROVIDER=mock QUICKSTART_OUT=./artifacts/v2-mock-run
make quickstart QUICKSTART_POSTPROCESS_PROVIDER=codex-headless-oauth QUICKSTART_POSTPROCESS_CONFIG=./postprocess.json QUICKSTART_POSTPROCESS_ALLOW_REMOTE=true
```

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
make bench-max-workers
make bench-process-shards
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

`batch` prints a compact tqdm-style progress line to stderr with per-PDF totals and live per-page OCR activity.

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

## Benchmark max_workers

From repository root:

```bash
make bench-max-workers VALUES=1,2,4,8
```

This writes per-run artifacts plus `summary.json` and `summary.md` under `artifacts/bench-max-workers` by default.

For `vision-swift`, auto mode now resolves to `2`, because higher in-process worker counts saturate inside Vision without materially improving throughput on `fixture_full.pdf`.

## Benchmark process shards

From repository root:

```bash
make bench-process-shards SHARDS=1,2,4,8 MAX_WORKERS_PER_SHARD=1
```

This launches separate provider processes over contiguous PDF page shards and writes `aggregate_report.json`, `combined_pages.json`, and `summary.{json,md}` under `artifacts/bench-process-shards` by default.

## Validate searchable PDF

From repository root:

```bash
make validate-searchable \
  SEARCHABLE=./artifacts/v2-vision-run/searchable.pdf \
  PAGES=./artifacts/v2-vision-run/pages.json \
  OUT=./artifacts/v2-vision-run/searchable_validation.json
```

The validator checks page count, non-blank extraction coverage, and per-page line-match consistency.

## Postprocess

- `ocrpoc-go run` and `ocrpoc-go batch` accept `--postprocess-provider` and `--postprocess-config`.
- `--ocr-local-only` controls the OCR provider network guard only; it defaults to `true`.
- `--postprocess-allow-remote` is required before remote postprocess providers are allowed.
- `--postprocess-config` falls back to `OCRPOC_POSTPROCESS_CONFIG` when omitted.
- The config file can select a named profile, resolve shared credentials via `auth_ref`, and override runtime settings like `output_mode`.
- `runtime.allow_remote=false` in the config still blocks remote postprocess providers even if the CLI flag is enabled.
- `output_mode=primary_artifacts` keeps `corrected_pages.json` sidecars and also regenerates `pages.json`, `document.txt`, `document.md`, and `searchable.pdf` from the corrected result.

Example with local OCR plus remote postprocess:

```bash
./bin/ocrpoc-go run \
  --input ../__fixtures__/fixture.pdf \
  --out ../artifacts/v2-vision-postprocess-run \
  --provider vision-swift \
  --ocr-local-only=true \
  --postprocess-provider codex-headless-oauth \
  --postprocess-config ../postprocess.json \
  --postprocess-allow-remote
```

Example config:

```json
{
  "version": "v1alpha1",
  "credentials": {
    "openai": {
      "kind": "oauth_store_file",
      "file": "~/.local/share/opencode/auth.json",
      "provider_id": "openai"
    }
  },
  "providers": {
    "default": {
      "provider": "codex-headless-oauth",
      "auth_ref": "openai",
      "output_mode": "primary_artifacts"
    }
  },
  "runtime": {
    "profile": "default"
  }
}
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
- `vision-swift` auto mode resolves to `2`; larger manual values still work, but OCR execution is capped internally at two active Vision workers per process.
- `ocrpoc-go run` and `ocrpoc-go batch` both show live OCR progress from provider events.
- `ocrpoc-go batch` shows live per-page OCR progress from the provider while the outer batch bar tracks PDF completion.
- `make validate-searchable` runs a PDFKit-based regression check against `pages.json`.
- Build can fail if local Swift toolchain and SDK are mismatched.
- Use `v2/providers/vision-swift/doctor.sh` to diagnose Swift toolchain issues.
- `doctor.sh`/`build.sh` can auto-select a compatible fallback SDK; override with `SWIFT_SDK_PATH` if needed.

## OCR local-only enforcement (v2)

- For `exec` providers, v2 monitors provider process tree network activity using `lsof` + `pgrep`.
- `local_only_report.json` is scoped to the OCR provider and includes `selfcheck_ok`, `monitor_samples`, and `remote_connection_violations`.
- If OCR local-only mode is enabled and remote violations are detected, command exits non-zero before postprocess runs.

## Outputs

- `run_report.json`
- `local_only_report.json`
- `batch_state.json`
- `batch_report.json`

## Worker behavior

- `--max-workers` omitted with `vision-swift` => auto mode (`2`)
- `--max-workers` omitted with other providers => auto mode (`cpu-1`, capped to 8)
- `--max-workers` set => manual mode
