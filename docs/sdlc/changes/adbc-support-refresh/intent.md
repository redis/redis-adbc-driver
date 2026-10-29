# Intent: refresh ADBC support comparison

Date: 2026-10-29. Requestor: repository user. Author: Codex.
Source: public main `5c1641f`. Status: prepared for review.
See [spec](spec.md) and [plan](plan.md).

Refresh support and testing status while preserving newer Airflow, Ray, Dask,
and dbt evidence. Distinguish recorded results from unverified candidates.
Runtime changes and new integration runs are outside scope.

October 29 follow-up: incorporate merged PRs #226–#229 from public main
`34ae626`; update pandas, community Dagster, and Polars claims without counting
overlapping suites twice. Preserve all 22 tools and previously linked evidence.

Further follow-up: include merged Prefect PR #230 from public main `71289eb`.
Keep custom Python ADBC tasks separate from prefect-redis and remote deployments.

Ray follow-up: incorporate merged PR #231 from public main `b59f25d`.
Recognize the worker-side ADBC datasource while retaining local-process, source-build,
live-read and retry limitations; keep older bridge validation separate.
