# Candidate test runner specification

- Date: 2026-10-30
- Source commit: `22fee6883b9440c0d9999e9f967bc390bb0fd0e3`
- Status: implemented; PR review pending
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Intent: [intent.md](intent.md)
- Plan: [plan.md](plan.md)

## Contract

- Start or reuse isolated Redis Software with `make -C go redis-up`.
- Verify active 4x QPF and effective Search settings before running tests.
- Build the native driver and set its library path for Python test processes.
- Run all suites by default or select tools by name before fixture setup.
- Run the local integration suites for NumPy, scikit-learn, pandas, Polars,
  Dask, Ray Data, DuckDB, Airflow, Dagster, Prefect, dlt, Jupyter, Hugging Face
  Datasets, Streamlit, and SciPy.
- Fetch stored typed rows with NULLs/Unicode in a separate notebook kernel,
  an AppTest app with a bound-filter rerun, and a locally persisted HF Dataset.
- Check a SciPy sparse solve against known results and its residual.
- Exercise the additional local Spark, model, plotting, orchestration, SDK,
  Superset, dbt, R, and Quarto paths listed in the [native sweep](native-sweep.md).
- Document the Python environment, dependencies, command, and container cleanup.

## Boundaries and risks

The suite validates local integration behavior only. Airflow's end-to-end smoke
test uses a temporary metadata database. DuckDB may download its Query.Farm
extension on first run. Hugging Face Datasets 5.1.0 needs `num_shards=1` to
save/reload empty results. Browser UI, hosted runtimes, and Flight SQL are not
covered. See [validation.md](validation.md) for the recorded limits.

## Acceptance checks

- The runner is executable and invokes all named test modules.
- The command is documented in the Python README.
- The four new workflow suites pass with no skips through the saved runner.
- The all-native launcher provisions compatible isolated environments, reports
  each launcher exit code and JUnit outcome, and continues independent groups
  after a setup failure.
- The complete runnable native set passes with the exact known R binding
  incompatibility and private-CA skip preserved, rather than hiding them.
- The [50-row native accounting](native-sweep.md) records integrations that
  still need implementation; they are not counted as successful tests.
- `git diff --check` passes.
