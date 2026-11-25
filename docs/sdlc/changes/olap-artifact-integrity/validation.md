# Validation

2026-11-24; base `12f46745`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Harness tests

`python -m unittest discover -s benchmarks/olap-release -p 'test_*.py'`:

- On unmodified main, the 8 new or changed tests fail (3 failures, 5 errors). With the zeroed DuckDB reference, the old validator reports only `invalid load key, '\x00'.`, the #641 symptom. A rewritten but decodable result is reported as `mismatch`. `dump_rows` returns no digest, and no retained files exist after a mismatch, an unreadable artifact or a validator timeout.
- With the change, all 30 tests pass on Python 3.14.10 (the CI version) and 3.15.2.
- The new tests cover:
  - 4,096 zero bytes appended to a result: located at the end of the real data, after 5 objects, with the zero run and recorded/read-back sizes reported, and the zero-filled file retained.
  - A zeroed reference: decode offset 0, and later engines are not run.
  - A rewritten but decodable result: `validation_failed` with a digest difference, not `mismatch`.
  - A missing digest.
  - A damaged scratch merge run.
  - Digest equal to the file bytes, with one fsync.
  - Retention for mismatch, artifact failure and validator timeout, with nothing retained on success.
- The fixtures that need NUL bytes write them explicitly (`bytes(4096)`) and assert that the file contains them.
- Validating 1M four-column rows (93.6 MB per file) takes 42.6 s before and 42.1 s after. Writing two such files takes 1.76 s before and 2.46 s after.

## TPC-H SF1 on Software Flex

Retained Redis Software 8.8.0 Flex database with TPC-H SF1 (verified 4x QPF, `adbc.redis.flex=true`). The harness ran in the release Linux ARM64 client container (Python 3.14.10, DuckDB 2.0.1) in the server's network namespace, limited to 4 CPUs and 8 GiB. The private profile, and so the artifacts, were on a Docker Desktop bind mount from macOS, as in the failing run. Driver: main `12f46745` (library sha256 `1bd2fb77…`), released `adbc_scanner` 98ff0a39fc. The preflight re-verified every table's row count.

| Query | Route | Status | Median | Timed runs |
|---|---|---|---:|---|
| Q11 (1,048 rows) | DuckDB native | ok | 8.6 ms | 9 / 7 / 9 ms |
| | Driver direct | ok | 2.11 s | 7.03 / 2.09 / 2.11 s |
| | DuckDB over the driver | ok | 16.51 s | 15.88 / 17.00 / 16.51 s |
| Q2 (100 rows) | DuckDB native | ok | 5.4 ms | 5 / 5 / 5 ms |
| | Driver direct | ok | 17.73 s | 17.73 / 16.78 / 18.08 s |
| | DuckDB over the driver | ok | 24.19 s | 25.69 / 24.19 / 23.55 s |

All 24 result files (2 queries × 3 routes × warm-up plus 3 timed runs) matched their recorded size and SHA-256 and validated. Nothing was retained.

Limits:
- One clean run does not show what produced the original zero bytes. The failing run used an earlier driver build (`fdd49f7c`), so these timings are not a before/after performance comparison.
- If the problem recurs, the record will now name the file and offset, and the files will be kept under `failed-artifacts/` in the private profile.
