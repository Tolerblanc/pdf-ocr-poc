# Evaluation Rules (v1)

This document defines deterministic normalization and scoring rules for the OCR POC.

## 1) Character normalization

- Apply Unicode NFC normalization to both reference and prediction.

## 2) Prose normalization

- Convert CRLF/CR to LF.
- Trim trailing spaces per line.
- Collapse repeated spaces/tabs into a single space.
- Preserve punctuation.

## 3) Code normalization

- Convert CRLF/CR to LF.
- Preserve symbols and in-line spacing.
- Ignore only trailing whitespace on each line.

## 4) Metrics

- KR prose CER and mixed KR/EN prose CER use `normalize_prose`.
- Code line accuracy uses line-normalized LCS matching to tolerate inserted noisy lines while preserving line order.
- Layout score uses macro F1 over expected block type presence.
- Reading-order check uses whitespace-normalized snippet order constraints in gold metadata.

## 5) Composite score

- CER quality: 40%
- Code-line accuracy: 25%
- Layout/block quality: 20%
- Throughput (pages/min): 15%

Candidate pass target in PRD: composite score >= baseline composite * 1.10.

## 6) CER scale

- CER values in reports are ratio scale (0..1), not percentages.

## 7) Blank-page policy

- Pages with no recognized text are marked as `is_blank=true` in page JSON.
- Non-empty extraction checks are applied to non-blank pages.
