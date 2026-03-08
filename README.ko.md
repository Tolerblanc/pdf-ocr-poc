# pdf-ocr-poc

Apple Silicon 환경에서 스캔된 한국어 기술 PDF를 처리하기 위한 로컬 전용 OCR CLI입니다.

English README: `README.md`

## 빠른 시작 (전체 빌드 + 첫 실행)

요구 사항:

- Apple Silicon(`arm64`) 기반 macOS
- Go 1.25+
- Swift가 포함된 Command Line Tools(또는 Xcode)

저장소 루트에서 실행:

```bash
# 1) Go/Swift 환경 점검
make doctor

# 2) CLI + Vision provider 빌드
make build-all

# 3) 선택 사항: local-only 모니터링 사전 조건 점검
./v2/bin/ocrpoc-go selfcheck-local-only

# 4) Apple Vision provider로 OCR 실행
./v2/bin/ocrpoc-go run \
  --input ./__fixtures__/fixture.pdf \
  --out ./artifacts/v2-vision-run \
  --provider vision-swift
```

`--out` 디렉터리에 생성되는 주요 결과물:

- `searchable.pdf`
- `pages.json`
- `document.txt`
- `document.md`
- `run_report.json`
- `local_only_report.json`

## 주요 기능

- 프로세스 트리 네트워크 모니터링(`lsof` + `pgrep`) 기반 local-only 실행 가드
- provider 기반 구조(`vision-swift`, `exec`, `mock`)
- 단일 PDF OCR 실행과 batch 처리 지원
- batch resume/retry 워크플로우(`continue + retry failed at end`)
- gold pages JSON 기준 평가 명령 지원
- 릴리스 아카이브와 Homebrew formula 초안 생성을 위한 패키징 도구

## 자주 쓰는 명령어

```bash
# Go CLI만 빌드
make build

# 테스트 실행
make test

# 스모크 테스트(run + batch + eval with mock provider)
make smoke

# fixture_full.pdf 기준 max_workers 벤치마크
make bench-max-workers VALUES=1,2,4,8

# PDF를 별도 프로세스 shard로 나눠 실행하는 벤치마크
make bench-process-shards SHARDS=1,2,4,8 MAX_WORKERS_PER_SHARD=1

# Batch OCR
./v2/bin/ocrpoc-go batch \
  --input ./__fixtures__ \
  --out ./artifacts/v2-batch \
  --provider vision-swift \
  --workers 2 \
  --retry-failed 1 \
  --resume

# Gold 데이터와 비교 평가
./v2/bin/ocrpoc-go eval \
  --gold ./fixtures/gold/v1/gold-pages.json \
  --pred ./artifacts/v2-vision-run/pages.json \
  --out ./artifacts/v2-vision-run/eval.json

# searchable PDF 추출 결과를 pages.json과 대조 검증
make validate-searchable \
  SEARCHABLE=./artifacts/v2-vision-run/searchable.pdf \
  PAGES=./artifacts/v2-vision-run/pages.json \
  OUT=./artifacts/v2-vision-run/searchable_validation.json
```

## Swift/SDK 문제 해결

`make doctor`가 Swift 단계에서 실패하면:

```bash
cd v2/providers/vision-swift
./doctor.sh
./build.sh
```

기본 SDK가 설치된 Swift 컴파일러와 맞지 않으면, 스크립트가 호환 가능한 fallback SDK를 자동으로 찾습니다. 필요하면 직접 지정할 수도 있습니다.

```bash
SWIFT_SDK_PATH=/Library/Developer/CommandLineTools/SDKs/MacOSX15.5.sdk make build-all
```

## 패키징

```bash
make package
make brew-formula URL=https://github.com/Tolerblanc/pdf-ocr-poc/releases/download/v0.1.0/ocrpoc-go-v0.1.0-darwin-arm64.tar.gz
```

추가 구현 메모: `v2/README.md`, `docs/v2-rewrite-plan.md`
