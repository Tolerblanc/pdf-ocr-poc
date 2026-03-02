# V2 Rewrite Plan (Go + Swift)

## Why rewrite

- Python runtime dependency is removable for distribution simplicity.
- Go improves CLI packaging, batch orchestration, state management, and concurrency control.
- Apple Vision is best accessed natively from Swift on macOS.

## Target architecture

- `ocrpoc` (Go): single user-facing CLI binary.
- `vision-provider` (Swift): Apple Vision OCR provider executable.
- `provider interface`: JSON request/response contract over stdin/stdout.

## Important note about "single binary"

- True one-binary delivery with Apple Vision is not practical if we keep Vision in Swift.
- Practical approach: one CLI command (`ocrpoc`) that manages provider binaries automatically.
- Packaging choices:
  - Bundle both binaries in one release archive (simplest).
  - Embed provider in Go binary and extract on first run (single-entry UX).

## Provider contract (initial)

Input (Go -> provider):

- `input_pdf`
- `output_dir`
- `profile` (dpi, langs, max_workers, quality flags)
- `local_only`

Output (provider -> Go):

- `searchable_pdf`
- `pages_json`
- `text_path`
- `markdown_path`
- `stage_timings`
- `warnings`

## Batch policy for v2

- Default policy: continue processing on errors.
- Retry failed files at the end (`retry_failed=1` default).
- Keep resumable run state on disk:
  - pending/running/succeeded/failed/retried
  - error class and retryability

## Milestones

1. `M1`: Go CLI skeleton (`run`, `batch`, `eval`) + state file + report output.
2. `M2`: Provider contract package + mock provider tests.
3. `M3`: Swift Vision provider MVP (OCR + artifact generation).
4. `M4`: Tesseract provider wrapper and parity checks.
5. `M5`: Performance tuning on `__fixtures__/fixture_full.pdf` and release packaging.

## Current status

- Completed: Go CLI skeleton for `run`, `batch`, and `eval` in `v2/`.
- Completed: Provider contract + `mock` provider + `exec` provider bridge.
- Completed: Provider contract schema draft at `v2/provider-contract.schema.json`.
- Completed: Batch state file with resume support and default retry-failed flow.
- Completed: Go tests ported from key Python cases (batch resume/retry/override, eval metric cases, CLI worker/platform guards).
- Completed: local-only monitoring in Go for `exec` providers (process-tree sampling via `lsof` + `pgrep`).
- Completed: `selfcheck-local-only` command in Go CLI for monitor prerequisite validation.
- Completed: Swift Vision provider implementation draft (`v2/providers/vision-swift/main.swift`) with OCR + artifact generation.
- Completed: repository cleanup to v2-centered layout (legacy v1 Python pipeline code removed).
- Pending: Swift provider runtime validation in this environment and `max_workers` page-level parallelization.
- Environment blocker observed: local Swift compiler/SDK mismatch prevents building Foundation-based binaries in current shell; provider scaffold is checked in but not executable until toolchain alignment.
- Added helper: `v2/providers/vision-swift/doctor.sh` for environment diagnostics.

## Acceptance for M1

- No Python dependency for CLI execution.
- Batch resume works after forced interruption.
- Failed jobs are retried at end and listed in final report.
- Output report includes elapsed time, pages/min, and stage timing placeholders.
