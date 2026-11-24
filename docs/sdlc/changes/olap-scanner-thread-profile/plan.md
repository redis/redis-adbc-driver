# Plan and evidence

See [intent](intent.md), [specification](spec.md).

1. Validate the private third-path-only profile before importing/opening DuckDB.
2. Omit default scanner options, route explicit numeric requests and retain
   existing driver options and fail-closed attach cleanup.
3. Extend existing mocked harness tests; keep operational runtime files untouched.

Four new mock tests cover missing/default/explicit-one omission, 16/1024 routing,
bad values before open, unchanged partition_count forwarding, no private
provenance, and unsupported-option failure without retry. All isolation tests
passed: `python3 -m unittest discover -s benchmarks/olap-release -p
'test_isolation.py'` (18 tests, 3.397s). Existing client-thread tests passed with
`test_client_threads.py` (3 tests, 0.008s). Diff checks pass. These are synthetic
adapters and child processes; no native SQL, Redis, container action, driver
build or benchmark occurred. Root review is required before publication/runtime
application. A live reader-concurrency claim is outside these mocked checks.
