# OLTP and selective-query performance

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
