# Controlled reads against the pre-snapshot main baseline

Date: 2026-11-04. Parent 2db2ff5. Full [parity](plan.md) remains incomplete.
[All successful raw runs and samples](pre-snapshot-read-benchmark.json).

Baseline b1d1bf005780516a17ceaf46ed49840506c77b20 is the current-main mutable
storage engine incorporated into this PR. Snapshot driver source is
02bd673b53b6416c1aaa38e5954d61b77b887abb, protocol 15. Both versioned native
libraries were built with make and Go 1.27.3 on the same macOS ARM64 host.
Python 3.14.10, PyArrow 26.0.1, ADBC driver manager 1.13.0 and redis-py 8.1.0
were shared by every run. Driver and exact harness hashes are in the raw data.

An owned strict-TLS Redis Software 8.2.0-78.18 / Redis 8.6.2 fixture used three
primary shards, noeviction and verified active effective 4x QPF. Initial management
TLS admission failed before loading/timing; installing the generated test
certificate on this owned management endpoint enabled strict verification for
all measured runs. The database and management requests verify the fixture CA.
Database-mutating suites completed before measurements. Native processes ran
sequentially in baseline, snapshot, snapshot, baseline order. Every invocation
verified policy and reset only this owned database before creating its dataset.
The final mutable dataset was cleared after all processes completed, so current
snapshot tests start from an empty compatible catalog; no migration was attempted.

Each run creates 20,000 rows plus 32 unrelated tables, updates id=1 to v=20,000
and deletes id=2, leaving 19,999 visible rows. Schemas permit NULLs but these
values contain none. Seven queries cover rejected/matching selective predicates,
grouping, distinct, descending top-K with OFFSET and a direct row-ID read.
One warmup precedes five recorded samples for each query in each process. The
table pools all ten recorded samples per revision/query across the two orders;
none are discarded. Timing includes execution, complete Arrow fetch/conversion
and comparison with independently calculated complete logical tuples. Setup,
connection initialization and reset are excluded. This is a synthetic read
workload, not ClickBench loading or a sustained concurrent-write measurement.

| Query | Mutable median ms | Snapshot median ms | Snapshot/mutable |
|---|---:|---:|---:|
| selective (no matching visible row) | 5.975 | 13.356 | 2.24x |
| matching selective | 3.780 | 14.287 | 3.78x |
| grouped count/sum | 4.700 | 15.366 | 3.27x |
| distinct count | 4.370 | 15.282 | 3.50x |
| top-K with default NULL order | 84.206 | 567.717 | 6.74x |
| top-K with explicit NULLS LAST | 3.982 | 14.421 | 3.62x |
| direct row-ID point read | 5.024 | 11.752 | 2.34x |

All four runs and all query results pass exact independent validation. These
numbers demonstrate remaining read overhead, not performance parity. The largest
gap has a concrete planner trigger: default DESC on the nullable v column rejects
index ordering, while explicit NULLS LAST permits it. The next optimization must
prove NULL absence against immutable visible rows before using that ordering;
suppressing NULL handling or ignoring replacement/tombstone visibility is unsafe.
The broader pin/metadata/read overhead also remains measurable in other cases.

The benchmark now reports its observed storage model/catalog protocol, with
mutable/NULL and snapshot/15 checked for all four runs. It hashes its own source.
It no longer reports an assumed physical segment count: initial_segments is NULL
and the report states that background maintenance was enabled during the workload.
Earlier JSON's initial_segments constants were inferred setup metadata, not a
sampled physical cardinality; retain those historical files with that limit.
Three Software primaries do not establish distributed managed-row storage:
current managed generations still share the metadata slot. This comparison does
not isolate every cause of overhead or prove a multi-shard throughput guarantee.

Reproduce each library invocation with snapshot_read_benchmark.py, --rows 20000,
--unrelated-tables 32, --repeats 5, the matching --driver/--source and owned local
management/data ports, and --reset-owned-database. Provide the private fixture URI
and trusted CA through the existing environment. Keep both revision orders and
all complete result checks. The exact committed harness hash matches every run.
Formatting/compilation and diff whitespace checks pass. Author review is not
independent maintainer approval. Full ordinary/platform/recovery, restricted
readers, public resume, sustained compaction and broader read/write/storage
comparisons remain required.
