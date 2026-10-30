# Candidate test runner plan and evidence

- Date: 2026-10-30
- Source commit: `22fee6849d0fdb49b894b67e26077d6a2f853b80`
- Status: implementation complete; PR review pending
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Intent: [intent.md](intent.md)
- Specification: [spec.md](spec.md)

## Work completed

1. Added an executable runner that starts/reuses Docker Redis 8.6.2 and builds
   the native driver.
2. Aggregated the existing local integration suites for NumPy, scikit-learn,
   pandas, Polars, Dask, Ray Data, DuckDB, Airflow, Dagster, Prefect, and dlt.
3. Added an optional dependency group for the Airflow provider and documented
   setup, rerun, and cleanup commands. Recorded the local NumPy, scikit-learn,
   and Dask results in the support matrix.

## Verification evidence

- `git diff --check` — passed.
- `python/tests/test_ml_ingest.py` — **2 passed** on macOS arm64 / Python 3.15.2
  against Redis OSS 8.6.2 with Search; NumPy 2.5.4 and scikit-learn 1.9.1.
- `python/tests/test_dask.py` — **8 passed** on the same host and Redis server
  with Dask/Distributed 2026.8.0.
- The aggregate runner was not executed. This host uses Python 3.15; the pinned
  Ray 2.59.0 and Dagster distributions are unavailable for that interpreter.
  Existing application-level evidence remains linked from the support matrix.
