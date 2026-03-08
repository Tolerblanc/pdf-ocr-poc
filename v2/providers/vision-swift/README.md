# vision-provider (Swift)

Swift executable provider for `ocrpoc-go --provider exec` integration.

Current state:

- Implements Apple Vision OCR (`VNRecognizeTextRequest`) over PDF pages.
- Writes contract artifacts: `searchable.pdf`, `pages.json`, `document.txt`, `document.md`.
- Searchable PDF now uses a CoreGraphics text-layer rewrite (no FreeText annotation overlays).
- `max_workers` now controls page-level parallel OCR workers.
- Auto mode now resolves to `2`; higher manual values are accepted but Vision execution is capped to two active OCR workers per process.
- Provider emits structured progress events so `ocrpoc-go batch` can show per-PDF/per-page progress.
- `validate_searchable_pdf.sh` checks page count, extraction coverage, and line-match consistency.
- Provider requests also accept optional `shard_index` / `shard_total` fields for separate-process shard benchmarks.

Build:

```bash
cd v2/providers/vision-swift
./doctor.sh
./build.sh
```

From repository root:

```bash
make doctor      # includes Swift doctor
make build-all   # Go + Swift provider
```

If build fails with Swift SDK/toolchain mismatch, align Xcode/CommandLineTools and retry.

Fallback behavior:

- `doctor.sh` auto-selects a compatible SDK when default SDK is incompatible.
- You can force SDK selection with `SWIFT_SDK_PATH`.

Example:

```bash
SWIFT_SDK_PATH=/Library/Developer/CommandLineTools/SDKs/MacOSX15.5.sdk ./build.sh
```

Binary path:

`v2/providers/vision-swift/bin/vision-provider`

Use with Go CLI:

```bash
cd v2
./bin/ocrpoc-go run \
  --input ../__fixtures__/fixture.pdf \
  --out ../artifacts/v2-vision-provider \
  --provider vision-swift \
  --provider-bin ./providers/vision-swift/bin/vision-provider
```

If `--provider-bin` is omitted, `ocrpoc-go` will try:

- `OCRPOC_VISION_PROVIDER_BIN`
- `v2/providers/vision-swift/bin/vision-provider`

Validate a generated searchable PDF:

```bash
v2/providers/vision-swift/validate_searchable_pdf.sh \
  --searchable-pdf ./artifacts/v2-vision-run/searchable.pdf \
  --pages-json ./artifacts/v2-vision-run/pages.json \
  --out ./artifacts/v2-vision-run/searchable_validation.json
```
