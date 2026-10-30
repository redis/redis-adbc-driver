# Candidate test runner plan and evidence

- Date: 2026-10-30
- Source commit: `22fee6883b9440c0d9999e9f967bc390bb0fd0e3`
- Status: implementation complete; PR review pending
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Intent: [intent.md](intent.md)
- Specification: [spec.md](spec.md)

Open compatibility/coverage follow-ups from the native sweep:
[#252](https://github.com/redis/redis-adbc-driver/issues/252),
[#253](https://github.com/redis/redis-adbc-driver/issues/253), and
[#254](https://github.com/redis/redis-adbc-driver/issues/254).

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
6. Completed the runnable native candidate sweep, adding plotting, model,
   local orchestration, Spark, R/DBI, Quarto, and existing dbt-adapter checks.
   Verified a manual RStudio session with the same saved R script.
7. Added an all-native launcher with isolated environments and per-group JUnit,
   logs, exit codes, and a JSON summary. Recorded all 50 table entries and their
   exact scope in [native-sweep.md](native-sweep.md).

## Verification evidence

- `git diff --check` — passed.
- `python/tests/test_ml_ingest.py` — **2 passed** on macOS arm64 / Python 3.15.2
  against Redis OSS 8.6.2 with Search; NumPy 2.5.4 and scikit-learn 1.9.1.
- `python/tests/test_dask.py` — **8 passed** on the same host and Redis server
  with Dask/Distributed 2026.8.0.
- The new four-tool runner selection passed **5 tests, zero skips** against
  Redis Software 8.6.2 with verified active 4x QPF; see [validation.md](validation.md).
- The initial attempt used Python 3.15, where pinned Ray/Dagster distributions
  were unavailable. The complete follow-up used isolated Python 3.12/3.14
  environments: **124 passed, one private-CA skip, one strict expected R binding
  failure**, every profile launcher exited 0. [Current evidence](native-sweep.md).
- Flight SQL was excluded at the user's request. Rows requiring a new native
  connector/adapter are explicitly listed as implementation gaps, not passes.
