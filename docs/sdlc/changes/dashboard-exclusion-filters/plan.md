# Implementation, validation and deployment

See [intent](intent.md) and [specification](spec.md).

1. Add term parsing and exclusion to `analytics/dashboard.js`, with help text in
   `dashboard.html`.
2. Add the presets and `traffic_selection` to `analytics/collector.py`; validate
   explicit classes against stored categories; add the missing-filter 503 to
   `analytics/serverless.py`.
3. Build a package from the deployed one, replacing only the changed files, and
   update the three functions' code. Do not use Terraform.

## Validation (2026-11-26)

- Tests were written first and failed before each change: two dashboard
  exclusion tests, the preset test (`KeyError: 'exclude_ci'`), the dashboard
  option list, and the older-generation 503 (`KeyError`). The category guard in
  `test_analytics.py` passed before the change and protects it.
- `node --test analytics/test_dashboard.cjs`: 8 passed.
- `python -m unittest test_serverless test_analytics test_backup` on Python 3.12
  without `ANALYTICS_TEST_REDIS_URL`: 29 run, OK, 14 skipped. The skipped tests
  are Redis-backed. They were not run because the only Redis Software fixture
  available belonged to a concurrent benchmark load. The changed `summarize`
  path is shared with the serverless tests, which ran.
- The same serverless and analytics tests passed when imported from the extracted
  package with its bundled dependencies (boto3/botocore 1.44.7, redis 8.1.0):
  27 run, OK, 14 skipped.

## Package

The deployed package (SHA-256 `rV6YDh9Jd8M6eD3oJVOLNWr9Mb8isFTwQqv2nREbm/s=`,
18,322,654 bytes) was downloaded from Lambda and verified. Its `collector.py`,
`serverless.py`, `dashboard.html` and `dashboard.js` matched PR #721's head
byte-for-byte. The new package replaces only those four entries and keeps every
other entry's bytes, order, timestamp and mode (2,529 entries). New SHA-256:
`yMuypDzXJMDqSQcfU0meO/cWD0FTk/DivaVwbqqIc9Y=`, 18,323,499 bytes. Both packages
remain private and outside git.

## Deployment (2026-11-26 UTC, approved by the requestor)

1. 21:22: `update-function-code` for `refresh` and `downloads`, both then
   `Active`/`Successful` with the new hash.
2. 21:23: one manual `refresh` invocation returned `status: ok` in 66.1 s with a
   529 MB peak (previous scheduled runs: 45–54 s, about 518 MB). The new
   generation's `summaries.json` has all eight filters and is 9,490,643 bytes
   (previously 5,654,112). For `all` and both presets, connections and attempts
   equal the sum of their kept categories. No reports are currently classified
   `application` or `benchmark`, so both presets currently equal `unknown`.
3. Before the API update, the old code served the larger file with a 174 MB
   peak. 21:26: `update-function-code` for `api`, `Active`/`Successful`.
4. Public checks: `/health` 200; the dashboard shell and script contain the
   presets and term parsing; `/api/summary` without a token 401; each of the
   eight filters 200 with matching `traffic_filter` and totals; an invalid filter
   400. API durations were 1.1–2.4 s with a 1.5 s cold start and a 169 MB peak;
   the summary error alarm stayed `OK`.

Rollback: upload the previous package to all three functions. The older API code
ignores the extra filters; the new API returns 503 for a filter missing from an
older generation.

Terraform: the legacy-retirement apply (PR #723, recorded in PR #724) last wrote
the shared state at 21:21:49 UTC, before this deployment. The state's three
`aws_lambda_function` entries still record the previous hash as both
`source_code_hash` and `code_sha256`. The live functions kept the new hash after
that apply. At 21:47 UTC, with the requestor's approval, the operator's private
`serverless_package` was replaced with the deployed package (hash verified); the
previous package is kept beside it as a backup. The next plan should therefore
show in-place code updates for the three functions that upload identical bytes,
which also brings the state hash in line. Never apply a package built from a
commit without these changes.
