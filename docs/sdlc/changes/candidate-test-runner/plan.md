# Candidate test runner plan and evidence

- Date: 2026-10-30
- Source commit: `22fee6883b9440c0d9999e9f967bc390bb0fd0e3`
- Status: implementation complete; PR review pending
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Intent: [intent.md](intent.md)
- Specification: [spec.md](spec.md)

## Work completed

1. Added an executable runner that provisions Redis Software and verifies active
   4x QPF through the repository setup, then builds the native driver.
2. Aggregated the existing local integration suites for NumPy, scikit-learn,
   pandas, Polars, Dask, Ray Data, DuckDB, Airflow, Dagster, Prefect, and dlt.
3. Added an optional dependency group for the Airflow provider and documented
   setup, rerun, and cleanup commands. Recorded the local NumPy, scikit-learn,
   and Dask results in the support matrix.
4. Added tool selection and a separate dependency extra for Jupyter, Hugging Face
   Datasets, Streamlit, and SciPy so these workflows can run independently.
5. Validated notebook execution, AppTest input reruns, typed Dataset persistence
   including empty results, and a sparse linear solve; [record](validation.md).

## Verification evidence

- `git diff --check` — passed.
- `python/tests/test_ml_ingest.py` — **2 passed** on macOS arm64 / Python 3.15.2
  against Redis OSS 8.6.2 with Search; NumPy 2.5.4 and scikit-learn 1.9.1.
- `python/tests/test_dask.py` — **8 passed** on the same host and Redis server
  with Dask/Distributed 2026.8.0.
- The new four-tool runner selection passed **5 tests, zero skips** against
  Redis Software 8.6.2 with verified active 4x QPF; see [validation.md](validation.md).
- The complete aggregate runner was not executed. This host uses Python 3.15; the pinned
  Ray 2.59.0 and Dagster distributions are unavailable for that interpreter.
  Existing application-level evidence remains linked from the support matrix.
