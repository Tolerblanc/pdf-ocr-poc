# vision-provider (Swift)

Swift executable provider for `ocrpoc-go --provider exec` integration.

Current state:

- Implements Apple Vision OCR (`VNRecognizeTextRequest`) over PDF pages.
- Writes contract artifacts: `searchable.pdf`, `pages.json`, `document.txt`, `document.md`.
- Current searchable PDF strategy uses PDF annotations with transparent text overlays.
- `max_workers` is accepted in contract but not yet used for page-level parallel OCR.

Build:

```bash
cd v2/providers/vision-swift
./doctor.sh
./build.sh
```

If build fails with Swift SDK/toolchain mismatch, align Xcode/CommandLineTools and retry.

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
