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


Explicit custom-runtime promotion follow-up: three additional mocks cover
boolean validation/custom-extension requirement before open, default/native
no-op, promotion of exactly the imported _duckdb binary before custom LOAD, and
path-free boolean provenance. Final test_isolation.py passed21 tests4.568s;
existing client-thread regressions passed3 tests0.010s. Real-child import-isolation
checks still assert Redis workers never import DuckDB. This does not claim an
extension runtime or live-query result; root owns the separate custom ABI proof.

Root independently reviewed dc08f9e3 runtime changes and approved publication.
A compatible custom upstream scanner is required for scan_threads above one;
Redis16-token profiles additionally require [#625](https://github.com/redis/redis-adbc-driver/pull/625)
so database-level adbc.redis.partition_count is inherited by the producer
statement. The harness does not choose or claim actual token/reader counts.
Root retains custom-extension commit/SHA proof privately; mocked tests deliberately
use synthetic scanner paths. No extra hash or live-result claim was introduced.
