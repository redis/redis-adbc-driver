# Measured numeric NULL-order optimization

Date: 2026-11-04. Implementation ac3c260. Full [parity](plan.md) remains incomplete.
The [correctness checkpoint](numeric-null-order-validation.md) describes the
visibility proof, exact fallbacks and error coverage.

Two sequential controlled native comparisons use the committed benchmark harness
and the same fixture, toolchain, data, complete independent result checks and
sampling method as the [earlier comparison](pre-snapshot-read-validation.md).
This is a synthetic workload, not ClickBench. Each process has 20,000 input rows,
19,999 visible rows, 32 unrelated tables, one warmup and five retained samples per
query. Tables pool ten samples per revision/query. All eight processes and all
seven query results per process completed successfully. No other database tests
or builds ran during either comparison. Strict management/data TLS, noeviction
and active effective 4x QPF were verified on the owned three-primary Redis
Software fixture. Three primaries do not distribute managed rows beyond their
shared metadata slot. Physical segment counts were not sampled; maintenance was
enabled. Raw files retain source, native artifact and harness hashes, every
sample, observed storage model/protocol and output counts.

## Before and after the optimization

[Raw before/after data](numeric-null-order-benchmark.json). Order: snapshot
02bd673, snapshot ac3c260, snapshot ac3c260, snapshot 02bd673. Both use protocol 15.

| Query | Before median ms | After median ms |
|---|---:|---:|
| selective, no matching visible row | 14.722 | 13.705 |
| matching selective | 14.156 | 14.300 |
| grouped count/sum | 14.799 | 14.895 |
| distinct count | 14.971 | 15.113 |
| top-K with default NULL order | 424.160 | 16.914 |
| top-K with explicit NULLS LAST | 13.854 | 13.654 |
| direct row-ID point read | 12.500 | 12.440 |

Default descending top-K improves **25.1x** in this paired cohort. The other six
cases are roughly unchanged. The earlier cohort's 567.717 ms is historical;
it is not the denominator for this paired speedup.

## Optimized snapshot versus mutable baseline

[Raw direct comparison](numeric-null-order-main-benchmark.json). Order: mutable
b1d1bf005780516a17ceaf46ed49840506c77b20, snapshot ac3c260, snapshot ac3c260,
mutable b1d1bf0. The baseline is the pre-snapshot main revision used in the
previous comparison, not a claim about today's latest upstream main. Observed
storage is mutable with no snapshot protocol, or snapshot protocol 15.

| Query | Mutable median ms | Optimized snapshot median ms | Snapshot/mutable |
|---|---:|---:|---:|
| selective, no matching visible row | 5.859 | 13.380 | 2.28x |
| matching selective | 3.738 | 14.312 | 3.83x |
| grouped count/sum | 4.318 | 15.585 | 3.61x |
| distinct count | 4.028 | 15.067 | 3.74x |
| top-K with default NULL order | 83.663 | 17.065 | 0.20x |
| top-K with explicit NULLS LAST | 3.989 | 13.523 | 3.39x |
| direct row-ID point read | 4.700 | 11.850 | 2.52x |

The optimized default top-K is faster than this mutable baseline. Six other
queries retain material overhead, so read performance parity is not established.
Pin/metadata/segment-validation round trips are a profiling target, not a proven
explanation of all the remaining cost. Broad write/storage comparisons,
sustained compaction, resume, current full ordinary/native/platform and Cluster
role/recovery evidence remain required. The final mutable dataset was cleared
only after its process completed, before starting current-driver regression
checks; no storage migration was attempted.

Every raw harness hash matches the committed harness; both revision orders,
all samples and complete independent validation are retained. Document links,
JSON parsing and diff whitespace were checked. Author review is not independent
maintainer approval.
