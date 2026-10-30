# Candidate test runner specification

- Date: 2026-10-30
- Source commit: `22fee6849d0fdb49b894b67e26077d6a2f853b80`
- Status: implemented; PR review pending
- Intent: [intent.md](intent.md)
- Plan: [plan.md](plan.md)

## Contract

- Start or reuse a named Redis 8.6.2 container on a configurable local port.
- Confirm Redis and Search are ready before running application tests.
- Build the native driver and set its library path for Python test processes.
- Run the existing local integration suites for NumPy, scikit-learn, pandas,
  Polars, Dask, Ray Data, DuckDB, Airflow, Dagster, Prefect, and dlt.
- Document the Python environment, dependencies, command, and container cleanup.

## Boundaries and risks

The suite validates local integration behavior only. Airflow's end-to-end smoke
test uses a temporary metadata database. DuckDB may download its Query.Farm
extension on first run. Hosted runtimes and Flight SQL are not covered.

## Acceptance checks

- The runner is executable and invokes all named test modules.
- The command is documented in the Python README.
- `git diff --check` passes.
