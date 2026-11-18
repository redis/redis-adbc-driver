# Intent

Date: 2026-11-18. Requestor: Albert Wong. Source: cacae16c637bd5459a43f9e794e8c80f7fafa08f. Status: implementation.

Move the native DuckDB extension to https://github.com/Redislabs-Solution-Architects/redis-adbc-duckdb and the Airflow, Dagster, Dask, Ray, Prefect, dlt and Superset integrations to https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations. Separate maintenance, dependencies and release gates from the core driver. Preserve core correctness tests and the upstream adbc_scanner integration.

See [spec](spec.md) and [plan](plan.md).
