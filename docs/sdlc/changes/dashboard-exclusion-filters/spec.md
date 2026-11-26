# Contract

See [intent](intent.md) and [plan](plan.md).

## Text exclusion (browser only)

- Table search, column filters and the execution-sample filter parse
  space-separated, case-insensitive terms. Every term must match: a plain term
  must appear (in any cell for search, in that cell for a column filter), and a
  `-`-prefixed term must not appear.
- Double quotes keep spaces or a literal leading minus (`"not reported"`,
  `-"ci / test"`, `"-5"`). A lone `-` or empty quotes add no term, so typing does
  not hide rows mid-word.
- Unquoted words now match independently rather than as one substring. Empty
  filters keep every row, as before.

## Traffic presets

- `exclude_ci` keeps `application`, `benchmark` and `unknown`;
  `exclude_ci_benchmark` keeps `application` and `unknown`. Both are added to
  `TRAFFIC_FILTERS`, so the refresh worker precomputes them with the existing
  filters and the API accepts them.
- `all` still keeps every row without category filtering. Single-category filters
  are unchanged.
- An explicit client `traffic_class` is accepted only from the five stored
  categories, so a filter name is never stored as a category.
- When the published generation lacks a requested filter (rollout or rollback),
  the API returns 503 with a retry message instead of failing with a 500.
- No payload, ingestion, storage format, schedule or infrastructure change.
