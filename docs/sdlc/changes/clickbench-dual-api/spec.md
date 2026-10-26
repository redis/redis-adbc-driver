# Specification

Three cases: narrow full-table projection; full-table numeric residual filter;
and a dense one-million-row-ID projection with numeric, text, DATE and TIMESTAMP
columns. Execute three trials per API and case, alternating API order. Both
paths include connection setup, full consumption and retained Arrow IPC writes.
Native timing includes planning and external process startup/teardown. Use the
same 16-vCPU client, 16 GiB hard cgroup limit, 16-worker budget and pinned driver
library. Preserve failures, timeouts and null ineligible timings.

Validate every output schema and row-ID uniqueness, cross-API exact multisets,
and expected counts/bounds. Full/filtered outputs receive complete independent
typed-Parquet multiset checks; bounded mixed outputs get full cross-API equality,
dense row-ID coverage and 100 independent source-membership samples. Do not
claim full-source correspondence for parallel-load row IDs. Retain raw files,
checksums, exact jobs, validation reports and explicit API capability results.
Only three valid paired trials qualify for median ratios. No SQL rewrite,
DuckDB execution inside the query timer, hidden fallback, or ClickBench ranking.

Wait for the original sweep and validation, then run supplemental scans. Gate
backup and approved teardown until supplemental work ends. Leave 90 minutes
before the fixed deadline for backup/restore/cleanup. The existing snapshot job
includes all supplemental artifacts and raw Arrow files. No driver or live
Redis data changes. See [intent](intent.md) and [plan](plan.md).
