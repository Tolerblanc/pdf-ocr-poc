# V2 OCR 개선 TODO (Vision Swift 중심)

마지막 업데이트: 2026-03-08

## 문서 목적

- v2 OCR 파이프라인의 품질/성능/운영 개선 항목을 한곳에서 관리한다.
- 특히 `fixture_full.pdf` 기준 회귀 이슈와 searchable PDF 사용자 경험 문제를 우선 추적한다.

## 상태 규칙

- `[ ]` TODO
- `[~]` 진행중
- `[x]` 완료

## 최근 반영된 항목

- `[x]` `max_workers` 미적용 상태를 CLI 경고로 명확히 표시 (`run`/`batch` 출력 개선)
- `[x]` searchable PDF 생성 시 `FreeText annotation overlay` 제거
- `[x]` searchable PDF 생성 경로를 CoreGraphics 기반 텍스트 레이어 방식으로 교체
- `[x]` batch tqdm 스타일 진행바에 PDF별/페이지별 진행 상태 표시 추가
- `[x]` Swift provider에서 `max_workers` 기반 page-level 병렬 OCR 구현
- `[x]` searchable PDF 텍스트 레이어 자동 검증 스크립트 추가

## 우선순위별 개선 항목

### P0 (가장 먼저)

- `[~]` **full fixture OCR 품질 회귀 원인 분석 및 수정**
  - 증상: `fixture_full.pdf` 결과 텍스트가 문맥 불일치/깨짐 비율이 높음
  - 참고: searchable PDF 자동 검증에서 pages `1, 93, 173, 213, 237`가 line-match 기준 미달
  - 완료 조건: `fixture_full.pdf` 샘플 페이지군(최소 20페이지)에서 수동 검수 + 자동 지표 동시 개선

- `[x]` **`max_workers` 실제 page-level 병렬 OCR 구현 (Swift provider)**
  - 반영: worker별 `PDFDocument`를 사용해 페이지 OCR을 병렬 처리
  - 확인 포인트: provider progress 이벤트와 함께 page-level 진행률이 batch 출력에 반영됨

- `[x]` **searchable PDF 텍스트 레이어 검증 스크립트 추가**
  - 검증: 페이지별 텍스트 추출 가능 여부, non-blank 페이지 커버리지, extraction consistency
  - 실행: `make validate-searchable SEARCHABLE=<pdf> PAGES=<pages.json>`

- `[ ]` **Preview/Acrobat 수동 QA 체크리스트 운영화**
  - 체크 항목: 드래그 선택 자연스러움, 검색 가능성, 복붙 품질, 이상 박스 유무
  - 완료 조건: `artifacts/*/manual_preview_check.md` 포맷 고정 및 주기적 기록

### P1 (품질 고도화)

- `[ ]` **Swift OCR stage 병렬화 스케일링 재프로파일링**
  - 관찰: `fixture_full.pdf` 기준 `w1 -> w8` end-to-end 속도 향상이 약 `1.05x`에 그침
  - 추가 관찰: `ocr_stage_profile.json` 상 `w8`도 `max_active_recognize_workers=2`에 머물고, 별도 프로세스 shard 실험(`s4-w1`)은 `35.0s`까지 단축됨
  - 완료 조건: render/Vision 단계 병목을 분해해 worker 증가에 따른 유의미한 OCR stage 단축 근거 확보

- `[ ]` **OCR 후보정(Post-correction) 레이어 추가 (요청사항 반영)**
  - 방향: OCR 결과를 자연스럽게 보정하는 후처리 레이어를 파이프라인에 추가
  - 목적: 오타/잘못된 인식/어색한 표현을 줄이고, 후보정된 텍스트 기준으로 searchable PDF를 재생성
  - 구현 원칙: 후보정 엔진을 provider 인터페이스화해서 사용자 취향대로 backend를 꽂을 수 있게 설계
  - 선행 검토:
    - 현재 `pages.json`/`document.txt`/`document.md` 구조가 후보정 입력으로 충분한지 재검토
    - 후보정은 가능하면 page/block 단위 structured JSON을 입력으로 사용하고, bbox/reading order/원문 텍스트 provenance를 유지
    - 저작권/원문 보존 관점에서 "자연스러운 후보정"과 "과도한 의역/재작성"의 경계를 정책으로 정의
    - 후보정 결과로 searchable PDF를 다시 만들 때 geometry는 OCR 원본을 최대한 유지하고, 텍스트만 교체하는 방식 우선 검토
    - provider/auth/config 설계는 n8n/OpenCode 류의 분리 패턴(credential storage vs provider config vs runtime override) 참고
  - 지원 모드:
    - `local-lm` (로컬 모델: 예: Ollama/llama.cpp)
    - `cloud-llm` (외부 LLM API, API Key 입력)
    - `foundation-models` (Apple Foundation Models 계열 로컬/시스템 API)
    - `codex-headless-oauth` (ChatGPT/Codex 계열 headless OAuth 기반 provider)
    - `none` (후보정 비활성)
  - 보안/운영:
    - API 키는 환경 변수로만 주입 (`OCRPOC_POSTPROC_API_KEY` 등)
    - `local_only=true`일 때는 `cloud-llm` 강제 차단
    - provider별 인증 방식(API key, OAuth token, local runtime)을 공통 설정 인터페이스로 추상화
  - 품질 가드:
    - 원문 대비 편집 거리 상한(과도한 재작성 방지)
    - 숫자/URL/코드 블록 보호 규칙
    - 페이지 단위 diff 로그 저장
    - 후보정 전/후 searchable PDF와 plain text를 모두 남겨 회귀 비교 가능하게 유지
    - block/page별 `source_text` -> `corrected_text` 매핑과 수정 사유/모델 메타데이터 저장
  - 참고 판단:
    - 한국어 OCR을 규칙 기반만으로 자연스럽게 후보정하는 것은 한계가 크므로, 규칙 기반은 보조 수단으로만 사용
  - 완료 조건: 후보정 on/off A/B 결과에서 CER/가독성 개선 + 환각/과보정 회귀 없음

- `[ ]` **규칙 기반 한국어 후보정(보조) 최소 세트 도입**
  - 범위: 띄어쓰기/문장부호/자주 발생하는 OCR 혼동 문자 일부만 제한적으로 보정
  - 완료 조건: false positive를 유의미하게 늘리지 않는 범위에서만 적용

- `[ ]` **reading order 및 block type 안정화**
  - 목표: TOC/도표/캡션/코드 혼합 페이지에서 순서 오류 감소
  - 완료 조건: gold subset 기준 reading_order_error_ratio 및 layout_macro_f1 개선

### P2 (운영/지속성)

- `[ ]` **품질 리포트 자동 생성 표준화**
  - 출력: run/eval 비교표 + 회귀 여부 + 경고 요약
  - 완료 조건: 릴리즈 전 체크리스트에 자동 포함

- `[ ]` **실험 아카이브 구조 정리**
  - 규칙: run id 네이밍, 보관 기간, baseline 보존 정책
  - 완료 조건: `artifacts/`와 `benchmarks/` 사용 규칙 문서화

- `[ ]` **장애 대응 런북 작성**
  - 내용: Swift SDK mismatch, provider crash, 품질 급락시 triage 절차
  - 완료 조건: 신규 환경에서도 문서만 보고 재현/복구 가능

## 제안 실행 순서

- 1단계: P0 항목(회귀 원인 + 병렬 OCR + PDF 검증 자동화)
- 2단계: P1 후보정 레이어 인터페이스 설계 및 `none/local-lm/cloud-llm` 모드 구현
- 3단계: P1 품질 가드/보조 규칙 적용 후 A/B 평가
- 4단계: P2 운영 문서/리포트 자동화로 고정
