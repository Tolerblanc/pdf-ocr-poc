# pdf-ocr-poc

Apple Silicon 환경에서 스캔된 한국어 기술 PDF를 처리하기 위한 로컬 전용 OCR CLI입니다.

English README: `README.md`

## 클론 후 첫 실행

요구 사항:

- Apple Silicon(`arm64`) 기반 macOS
- Go 1.25+
- Swift가 포함된 Command Line Tools(또는 Xcode)

```bash
git clone https://github.com/Tolerblanc/pdf-ocr-poc.git
cd pdf-ocr-poc
make quickstart
```

`make quickstart`는 doctor 점검, CLI 빌드, 필요 시 번들 Vision provider 빌드, OCR local-only 사전 점검, `__fixtures__/fixture.pdf` OCR 실행까지 한 번에 수행하고 결과를 `artifacts/v2-quickstart`에 남깁니다.

자주 쓰는 오버라이드 예시:

```bash
# 내 PDF를 원하는 출력 경로로 실행
make quickstart \
  QUICKSTART_INPUT=./my.pdf \
  QUICKSTART_OUT=./artifacts/my-run

# Swift 빌드를 건너뛰고 mock provider로 CLI 동작만 확인
make quickstart \
  QUICKSTART_PROVIDER=mock \
  QUICKSTART_OUT=./artifacts/v2-mock-run

# OCR은 local-only로 유지하고, 후보정만 원격 허용
make quickstart \
  QUICKSTART_POSTPROCESS_PROVIDER=codex-headless-oauth \
  QUICKSTART_POSTPROCESS_CONFIG=./postprocess.json \
  QUICKSTART_POSTPROCESS_ALLOW_REMOTE=true
```

로컬 후보정 설정 파일은 이렇게 시작하면 됩니다:

```bash
cp ./postprocess.example.json ./postprocess.json
```

- `postprocess.example.json`은 레포에 포함된 Codex 후보정용 스윗스팟 프로필입니다.
- 기본값은 OpenCode 인증 저장소 `~/.local/share/opencode/auth.json`을 사용합니다.
- 인증 파일 경로가 다르면 `postprocess.json`의 `credentials.openai.file` 값을 수정하면 됩니다.
- 환경 변수 기반 OAuth 토큰을 쓰고 싶다면 credential `kind`를 `env_oauth_access_token`으로 바꾸고 `OCRPOC_POSTPROCESS_CODEX_ACCESS_TOKEN`과 필요 시 `OCRPOC_POSTPROCESS_CODEX_REFRESH_TOKEN`, `OCRPOC_POSTPROCESS_CODEX_ACCOUNT_ID`를 설정하면 됩니다.

직접 단계별로 실행하려면 저장소 루트에서:

```bash
# 1) Go/Swift 환경 점검
make doctor

# 2) CLI + Vision provider 빌드
make build-all

# 3) 선택 사항: OCR local-only 모니터링 사전 조건 점검
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
- `local_only_report.json` (OCR provider 전용)

## 주요 기능

- 프로세스 트리 네트워크 모니터링(`lsof` + `pgrep`) 기반 OCR provider local-only 실행 가드
- provider 기반 구조(`vision-swift`, `exec`, `mock`)
- `--postprocess-allow-remote`로만 여는 선택적 원격 후보정 레이어
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

# OCR은 local-only로 유지하고, 후보정만 원격 허용
./v2/bin/ocrpoc-go run \
  --input ./__fixtures__/fixture.pdf \
  --out ./artifacts/v2-vision-postprocess-run \
  --provider vision-swift \
  --ocr-local-only=true \
  --postprocess-provider codex-headless-oauth \
  --postprocess-config ./postprocess.json \
  --postprocess-allow-remote
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
