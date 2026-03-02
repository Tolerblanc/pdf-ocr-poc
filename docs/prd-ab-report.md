# PRD A/B Candidate Report

- Generated at: 2026-03-02T15:38:42
- Report type: candidate comparison (winner not fixed)
- Fixture: `__fixtures__/fixture.pdf` (21 pages)

## Baseline

- Run dir: `benchmarks/baseline-tesseract-ocrmypdf`
- Engine: `tesseract`
- Runtime: `44.495s` (28.318 pages/min)
- Composite score: `0.501255`

## Candidate Summary

| Candidate | Engine | Runtime(s) | Pages/min | KR CER | KR/EN CER | Code Acc | Layout F1 | Reading Err | Composite | Improvement vs Baseline |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| paddle-fast-v3 | paddle | 219.151 | 5.749 | 0.3468 | 0.5288 | 1.0000 | 0.8020 | 0.0000 | 0.665720 | 32.81% |
| apple-vision-fast-v2 | apple-vision | 34.328 | 36.705 | 0.0605 | 0.5191 | 1.0000 | 0.8833 | 0.0000 | 0.905167 | 80.58% |

## PRD Gate Snapshot

| Candidate | AC-01 Local-only | AC-03 KR CER | AC-03 KR/EN CER | AC-04 Code | AC-05 Layout | AC-05 Reading Order | AC-06 Non-Blank Text | AC-08 Fast Runtime | Composite >= +10% |
|---|---|---|---|---|---|---|---|---|---|
| paddle-fast-v3 | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS |
| apple-vision-fast-v2 | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS |

## Artifact Paths

- Baseline run: `benchmarks/baseline-tesseract-ocrmypdf`
- Candidate run (paddle-fast-v3): `benchmarks/candidate-paddle-fast-v3`
- Candidate run (apple-vision-fast-v2): `benchmarks/candidate-apple-vision-fast-v2`
