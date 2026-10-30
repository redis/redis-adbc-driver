# Candidate test runner intent

- Date: 2026-10-30
- Source commit: `22fee6849d0fdb49b894b67e26077d6a2f853b80`
- Status: implementation complete; PR review pending
- Requestor: user
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)

## Problem

The repository contains integration suites for multiple data engineering
applications, but rerunning the candidate paths requires manual Redis setup,
native driver configuration, and remembering the relevant test modules.

## Desired outcome

Provide one documented command to start or reuse a disposable Redis server and
run the existing local candidate integration suites together.

## Scope

Aggregate the existing NumPy, scikit-learn, pandas, Polars, Dask, Ray Data,
DuckDB, Airflow, Dagster, Prefect, and dlt tests in a repeatable Docker-backed
runner. Keep the runner local: Flight SQL, hosted products, Spark/Databricks,
and remote orchestration workers are outside its scope.

## Links

- Specification: [spec.md](spec.md)
- Plan and evidence: [plan.md](plan.md)
