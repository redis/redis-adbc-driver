# Validation: snapshot read and shared-partition milestone

Date: 2026-11-02. Implementation: `9bc80c0` (parent `b4b73e0`).
Status: milestone evidence; the full [parity plan](plan.md) remains incomplete.

For the later protocol-16 Cluster API, native C ABI and persistence checks, see
the separate [current Cluster checkpoint](cluster16-validation.md). Results below
retain their original source revision and scope.

## Environment and scope

Owned disposable Redis Software 8.2.0-78.18, Redis 8.6.2, Search enabled,
one Software node with three primary shards, proxy TLS with a verified private
CA, active management-verified 4x QPF and noeviction. The catalog's metadata and
rows still share one hash slot. This is proxy evidence, not a rerun of the prior
three-node Cluster API/failover/AOF checks.

Host: macOS ARM64, Apple M5 Max, 36 GiB RAM. Docker runtime: 18 CPUs,
25,421,033,472 bytes memory. Go 1.27.2; native ADBC driver-manager 1.13.0,
PyArrow 26.0.0. Native libraries built with `make build` and repository version
flags. Operational identities, endpoints, certificates and logs remain private.

## Verified behavior

- Shared live partition sets survive producer close, DML, ALTER, TRUNCATE,
  DROP/recreation and compaction. Expiry, tampering, legacy descriptor rejection,
  explicit release, active child-reader survival and pin cleanup are checked.
- Selective Search discovery fetches only surviving candidate HASHes, checking
  newer exact directories even when the newest replacement no longer matches.
  Missing shadowing HASHes fail explicitly. Lookup joins and correlated EXISTS
  use the same visible values; parallel scalar reduction obtains multiple workers.
- Bounded masks preserve visible index GROUP BY, weighted AVG, NULL/boolean
  reducers, DISTINCT/FILTER, grouping sets and large tuple aggregation. The
  70,001-row case independently computes expected groups, distinct sets and sums.
  Oversized masks fall back without resurrecting old rows.
- Ordered scans merge exact per-segment values and apply OFFSET/LIMIT once.
  Tests cover rounded BIGINT ties above 2^53, multiple sort keys, NULL placement,
  residuals and late output projection.
- Acquisition checks metadata structure; referenced source validation is cached
  once per immutable descriptor per statement, including concurrent callers.
  An unavailable unrelated index no longer blocks a healthy-table query.
  Referenced unavailable indexes and missing published point rows still fail.

## Reproduced findings and corrections

Late ordered projection initially omitted correlated outer-column dependencies;
the EXISTS regression reproduced and passed after carrying those dependencies.
Point-read integrity testing exposed a separate materialized lookup shortcut
that silently omitted a missing HASH; both materialized and iterator direct
lookups now resolve authoritative snapshot visibility.

The private-index readiness fault-injection hook initially targeted any index
read in a writing context. Lazy shared-source validation made it inject a
rebuilding state into a published input. The hook now targets only owned private
indexes; its original assertion that private readiness waits for indexing is
preserved and passes. Grouped aggregation's initial expected-order failure was
a test sorting error; independently calculated values were unchanged.

## Checks

- Unit packages: `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -json ./...`:
  891 test pass events, 650 expected integration skip events, no failures.
- Native `validation/snapshot_validation.py`: all 10 tests passed, including
  shared live generation after producer close and table recreation.
- Focused race checks for lazy validation, concurrent validation caching, missing
  baseline point rows, nested reads and private-index readiness passed.
- Complete proxy snapshot race suite: `go test -race -count=1 -v -run '^TestSnapshot' .`: 66 pass events, 7 expected Cluster API skip events, no failures (114.221 seconds). Cluster API cases require their separately verified fixture and remain pending for this revision.

## Remaining scope

Incremental metadata sharing/publication, independent-writer conflict handling,
one-build index lifecycle, resumable large atomic operations and sustained
compaction improvements remain open. Full legacy feature regression, native
platform matrix and new Cluster API/recovery runs remain open. Read benchmarks
against the preceding snapshot revision do not establish parity with the
pre-snapshot driver or all workloads. No full feature/performance parity claim
is made by this milestone.

## Comparative read microbenchmark

[Raw samples and revisions](read-benchmark.json). Native C/ADBC library for
`b4b73e0` versus `9bc80c0`, each built with the same version flags/toolchain.
Final run order: baseline, then current, in separate processes. Reset the owned
database between revisions because storage formats differ. Same Software fixture,
20,000 initial rows, 19,999 visible rows after one replacement and one deletion,
three immutable segments, 32 unrelated empty tables. One warmup per case followed
by five samples. Each sample includes execute, complete Arrow fetch, conversion
and independently calculated result comparison; setup/loading is excluded.

| Case | Baseline median ms | Current median ms | Baseline/current |
|---|---:|---:|---:|
| Predicate whose old match was replaced | 146.23 | 11.76 | 12.43× |
| Two surviving matches | 179.91 | 13.32 | 13.50× |
| Grouped COUNT/SUM | 170.35 | 13.37 | 12.74× |
| COUNT DISTINCT | 124.46 | 13.96 | 8.91× |
| Descending top-K, default NULL order | 647.21 | 620.95 | 1.04× |
| Descending top-K, explicit NULLS LAST | 642.33 | 12.89 | 49.82× |
| Exact row-ID lookup | 40.11 | 10.69 | 3.75× |

Default descending NULL ordering uses the full-sort fallback and shows no
meaningful gain in this run. Explicit NULLS LAST admits the visibility-masked
ordered merge. Selective/aggregate gains include both query-path improvements
and avoiding unrelated-catalog index checks; this experiment does not isolate
those contributions. These local, warm microbenchmarks are not pre-snapshot
parity, sustained-write throughput, distributed scaling or platform-matrix
measurements. Wide outputs, large masks and other shapes still need comparisons.

Reproduce with `validation/snapshot_read_benchmark.py --driver <library>
--source <revision> --software-api-port <owned API port>
--software-data-port <owned published proxy port> --reset-owned-database`, from
`go/`, with `REDIS_SNAPSHOT_URI` and `SSL_CERT_FILE` for the verified disposable
fixture. The script verifies Software QPF/noeviction and requires an explicitly
acknowledged local database reset; never use a shared database.
