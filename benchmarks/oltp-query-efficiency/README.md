# OLTP and selective-query performance

The [November 14 comparison](2026-11-14/report.md) adds the next point/write/retrieval
changes, all 42 comparisons and explicit concurrency regressions. Both 168-record
matrices and actual-source checks/cleanup are retained and independently audited.

The [November 13 comparison](2026-11-13/report.md) retains 168 validated records
per build, all 42 chart combinations, profiling attribution and actual verification
sources/skips. It compares clean ca685806 with the versioned unreleased candidate;
these are finite workload measurements, not a universal ranking or capacity claim.

```sh
python3 benchmarks/oltp-query-efficiency/audit.py
python3 -m unittest discover -s benchmarks/oltp-query-efficiency -p 'test_*.py'
```

`compare.py --baseline FILE --candidate FILE --out NEW_DIRECTORY` audits complete,
matching matrices and emits JSON/CSV with both builds' p50/p95, finite throughput,
sample counts and redis-py multiples. It rejects missing/duplicate/failed trials,
incompatible timing/fixtures/packages/harnesses and inconsistent statistics. It
is offline and never starts Redis or ClickBench.

For new evidence, use `benchmarks/table-modes/scenarios.py` with versioned driver
and CLI builds and a fresh private work directory on an owned empty Redis Software
fixture first configured by `SOFTWARE_SHARDS=1 make -C go redis-up`. Set that owned
database to 4 GiB through its management API before running the matrix. Independently verify effective
4x QPF, six workers/mnp32, strict data TLS/RESP3 and actual resources before timing.
Default execution records one warmup and three measured trials across managed,
unmanaged and redis-py, with one/four clients. It fully fetches typed Arrow results
and independently validates values and complete mutation post-state. Clients are
persistent within trials; internal driver setup is included in operation latency.
The retained ownership marker is for owner cleanup after every client/process ends.

The opt-in Go tests `TestOLTPStatementReadPoolProfile` and
`TestUnmanagedAggregateExecutionProfile` provide separate diagnostic attribution.
The latter requires `REDIS_UNMANAGED_AGG_PROFILE=true`; optional private output/CPU
paths use `REDIS_UNMANAGED_AGG_PROFILE_DIR` and `REDIS_UNMANAGED_AGG_CPU_PROFILE`.
These clear their disposable snapshot fixture. Run them in a separate verified
Software slot, never concurrently with native timing. Raw diagnostics/CPU profiles
may expose operational paths or input data and should remain private.

## Audit another complete paired run

The original `audit.py` default and November 13 artifacts stay unchanged. New
full 168-record/build bundles use:

```sh
python3 benchmarks/oltp-query-efficiency/audit_pair.py --bundle PATH_TO_BUNDLE \
  --require-check ordinary-software --require-check snapshot-software
```

The default bundle path is `2026-11-14`. The audit exactly recalculates all 42
JSON/CSV comparisons, source metadata, settings and input hashes with `compare.py`.
It requires baseline/candidate raw records, comparison JSON/CSV, `provenance.json`,
`checks.json`, `environment.json`, `cleanup.json`, `report.md`, frozen copies at
`harness/run.py` and `harness/scenarios.py`, and a complete `sha256.json` manifest
of every bundle file except the manifest itself. Extra diagnostic files are
hash-checked; their specialized statistical audits remain separate.

Provenance retains `driver_builds` (both commit/library/CLI/harness identities),
`build_artifacts_after_run` with both builds' `library_sha256`/`cli_sha256`,
`compare_source_sha256`, `post_measurement_production_changes=false`, and zero
`automatic_retries`, `failed_native_trial_records` and
`discarded_native_trial_records`. Each recorded checkpoint retains its actual
`source_commit`, exit code, passed/failed/skipped top-level counts and complete
`skipped_tests`; checkpoints may legitimately come from different commits.
Use repeated `--require-check` arguments to enforce the intended check inventory.
Environment and cleanup use the documented bundle field names; cleanup must
record whether TLS was generated and prove its removal or preexisting preservation.
This command is offline and never loads a driver, invokes a harness or starts Redis.
