SHELL := /usr/bin/env bash

ROOT_DIR := $(abspath .)
V2_DIR := $(ROOT_DIR)/v2
V2_BIN_DIR := $(V2_DIR)/bin
V2_BIN := $(V2_BIN_DIR)/ocrpoc-go
VISION_DIR := $(V2_DIR)/providers/vision-swift
VISION_BIN := $(VISION_DIR)/bin/vision-provider
DIST_DIR := $(ROOT_DIR)/dist
VERSION ?= $(shell git describe --tags --always --dirty)
ARCHIVE := $(DIST_DIR)/ocrpoc-go-$(VERSION)-darwin-arm64.tar.gz

.PHONY: help
help:
	@printf "Targets:\n"
	@printf "  make doctor           # Run Go + Swift environment checks\n"
	@printf "  make build            # Build Go CLI only\n"
	@printf "  make build-all        # Build Go CLI + Swift provider\n"
	@printf "  make test             # Run Go tests\n"
	@printf "  make smoke            # Run smoke tests with mock provider\n"
	@printf "  make bench-max-workers [VALUES=1,2,4,8]  # Benchmark fixture_full.pdf across max_workers values\n"
	@printf "  make bench-process-shards [SHARDS=1,2,4,8] [MAX_WORKERS_PER_SHARD=1]  # Benchmark per-process PDF shards\n"
	@printf "  make validate-searchable SEARCHABLE=<pdf> PAGES=<json> [OUT=<report.json>]  # Validate searchable PDF extraction\n"
	@printf "  make package          # Create release tarball under dist/\n"
	@printf "  make brew-formula URL=<release-url>  # Generate Homebrew formula\n"
	@printf "  make clean            # Remove build outputs\n"

.PHONY: doctor doctor-go doctor-swift
doctor: doctor-go doctor-swift

doctor-go:
	@echo "[doctor-go]"
	@go version
	@cd "$(V2_DIR)" && go env GOOS GOARCH

doctor-swift:
	@echo "[doctor-swift]"
	@"$(VISION_DIR)/doctor.sh"

.PHONY: build build-go build-vision build-all
build: build-go

build-go:
	@echo "[build-go]"
	@mkdir -p "$(V2_BIN_DIR)"
	@cd "$(V2_DIR)" && go build -o "$(V2_BIN)" ./cmd/ocrpoc-go

build-vision:
	@echo "[build-vision]"
	@"$(VISION_DIR)/build.sh"

build-all: build-go build-vision

.PHONY: test
test:
	@echo "[test]"
	@cd "$(V2_DIR)" && go test ./...

.PHONY: smoke smoke-run smoke-batch smoke-eval
smoke: smoke-run smoke-batch smoke-eval

smoke-run: build-go
	@echo "[smoke-run]"
	@cd "$(V2_DIR)" && ./bin/ocrpoc-go run --input "../__fixtures__/fixture.pdf" --out "../artifacts/v2-smoke-make" --provider mock

smoke-batch: build-go
	@echo "[smoke-batch]"
	@cd "$(V2_DIR)" && ./bin/ocrpoc-go batch --input "../__fixtures__" --out "../artifacts/v2-batch-make" --provider mock --workers 2 --retry-failed 1 --resume

smoke-eval: build-go
	@echo "[smoke-eval]"
	@cd "$(V2_DIR)" && ./bin/ocrpoc-go eval --gold "../fixtures/gold/v1/gold-pages.json" --pred "../artifacts/v2-smoke-make/pages.json" --out "../artifacts/v2-smoke-make/eval.json"

.PHONY: bench-max-workers
bench-max-workers: build-all
	@echo "[bench-max-workers]"
	@VALUES="$${VALUES:-1,2,4,8}"; \
	OUT_ROOT="$${OUT_ROOT:-$(ROOT_DIR)/artifacts/bench-max-workers}"; \
	cd "$(V2_DIR)" && go run ./cmd/ocrpoc-benchmax --input "../__fixtures__/fixture_full.pdf" --out-root "$$OUT_ROOT" --values "$$VALUES"

.PHONY: bench-process-shards
bench-process-shards: build-all
	@echo "[bench-process-shards]"
	@SHARDS="$${SHARDS:-1,2,4,8}"; \
	MAX_WORKERS_PER_SHARD="$${MAX_WORKERS_PER_SHARD:-1}"; \
	OUT_ROOT="$${OUT_ROOT:-$(ROOT_DIR)/artifacts/bench-process-shards}"; \
	cd "$(V2_DIR)" && go run ./cmd/ocrpoc-benchshard --input "../__fixtures__/fixture_full.pdf" --out-root "$$OUT_ROOT" --shards "$$SHARDS" --max-workers-per-shard "$$MAX_WORKERS_PER_SHARD"

.PHONY: validate-searchable
validate-searchable:
	@if [[ -z "$(SEARCHABLE)" || -z "$(PAGES)" ]]; then echo "SEARCHABLE and PAGES are required. Example: make validate-searchable SEARCHABLE=./artifacts/v2-vision-run/searchable.pdf PAGES=./artifacts/v2-vision-run/pages.json OUT=./artifacts/v2-vision-run/searchable_validation.json"; exit 1; fi
	@echo "[validate-searchable]"
	@CMD=("$(VISION_DIR)/validate_searchable_pdf.sh" --searchable-pdf "$(SEARCHABLE)" --pages-json "$(PAGES)"); \
	if [[ -n "$(OUT)" ]]; then CMD+=(--out "$(OUT)"); fi; \
	if [[ -n "$(MIN_COVERAGE)" ]]; then CMD+=(--min-coverage "$(MIN_COVERAGE)"); fi; \
	if [[ -n "$(MIN_LINE_MATCH)" ]]; then CMD+=(--min-line-match "$(MIN_LINE_MATCH)"); fi; \
	"$${CMD[@]}"

.PHONY: package
package: build-go
	@echo "[package]"
	@mkdir -p "$(DIST_DIR)"
	@TMP_DIR="$$(mktemp -d)"; \
	  cp "$(V2_BIN)" "$$TMP_DIR/ocrpoc-go"; \
	  if [[ -x "$(VISION_BIN)" ]]; then cp "$(VISION_BIN)" "$$TMP_DIR/vision-provider"; fi; \
	  tar -C "$$TMP_DIR" -czf "$(ARCHIVE)" .; \
	  rm -rf "$$TMP_DIR"; \
	  echo "created: $(ARCHIVE)"

.PHONY: brew-formula
brew-formula: package
	@if [[ -z "$(URL)" ]]; then echo "URL is required. Example: make brew-formula URL=https://github.com/Tolerblanc/pdf-ocr-poc/releases/download/v0.1.0/ocrpoc-go-v0.1.0-darwin-arm64.tar.gz"; exit 1; fi
	@echo "[brew-formula]"
	@SHA="$$(shasum -a 256 "$(ARCHIVE)" | awk '{print $$1}')"; \
	  OUT="$(DIST_DIR)/ocrpoc-go.rb"; \
	  { \
	    echo 'class OcrpocGo < Formula'; \
	    echo '  desc "Local-only OCR CLI for Korean technical PDFs"'; \
	    echo '  homepage "https://github.com/Tolerblanc/pdf-ocr-poc"'; \
	    echo '  url "$(URL)"'; \
	    echo '  sha256 "'"$$SHA"'"'; \
	    echo '  license "MIT"'; \
	    echo; \
	    echo '  def install'; \
	    echo '    bin.install "ocrpoc-go"'; \
	    echo '    if File.exist?("vision-provider")'; \
	    echo '      libexec.install "vision-provider"'; \
	    echo '      (bin/"vision-provider").write_env_script(libexec/"vision-provider", {})'; \
	    echo '    end'; \
	    echo '  end'; \
	    echo; \
	    echo '  def caveats'; \
	    echo '    <<~EOS'; \
	    echo '      To use bundled vision provider:'; \
	    echo '        export OCRPOC_VISION_PROVIDER_BIN="#{opt_libexec}/vision-provider"'; \
	    echo '    EOS'; \
	    echo '  end'; \
	    echo; \
	    echo '  test do'; \
	    echo '    output = shell_output("#{bin}/ocrpoc-go help")'; \
	    echo '    assert_match "ocrpoc-go", output'; \
	    echo '  end'; \
	    echo 'end'; \
	  } > "$$OUT"
	@echo "generated: $(DIST_DIR)/ocrpoc-go.rb"

.PHONY: clean
clean:
	@echo "[clean]"
	@rm -rf "$(V2_BIN_DIR)" "$(VISION_DIR)/bin" "$(DIST_DIR)"
