# Gold annotations

Gold annotations for evaluation live under `fixtures/gold/v1/`.

- `gold-pages.json` provides page-level references used by `scripts/eval_metrics.py`.
- The file currently covers 12+ pages sampled across TOC, prose, mixed KR/EN, code, and diagram-heavy pages.

Update policy:

1. Preserve `version` and increment directory version for schema changes.
2. Keep snippet-based references deterministic and UTF-8 encoded.
3. Ensure new gold entries include `page`, and at least one measurable field.
