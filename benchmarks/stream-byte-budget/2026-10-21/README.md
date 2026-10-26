# Optional accounted-byte stream batches: #410

2026-10-21. [Change records](../../../docs/sdlc/changes/remaining-performance/intent.md),
[manifest](manifest.json), [issue #188](https://github.com/redis/redis-adbc-driver/issues/188).
Baseline runtime: merged #422 (`7a42b2d`); candidate production source `8a3f846`.

## Behavior and rationale

`adbc.redis.stream_batch_bytes` adds an opt-in accounted-byte ceiling alongside
the existing row ceiling. **Default 0 keeps row-only batching for speed.** Both
public APIs and generic streamed expressions support it. A planner statement's
settings do not travel in descriptors: partition readers configure their database.

Each projected value accounts 256 bytes plus twice string/binary payload,
decimal magnitude plus 32 bytes, and recursively held record/array children.
NULL slots and duplicate outputs count. Coercion occurs before accounting.
A batch stops before adding a row that would cross the budget; one oversized
row is admitted alone to ensure progress. The row ceiling also applies.
Accounting sums saturate rather than wrap. This is a deterministic sizing unit,
**not an allocator measurement, hard memory limit, or process RSS cap**.

A cursor page is still fetched whole. Raw input, decoded generic pending rows
and completed Arrow batches from that page may coexist. Builder capacity/slack,
consumer-retained batches and a single huge row remain outside a process bound.
The byte setting also caps initial Arrow row reservation; generic pending-byte
updates are linear in ordinary operation. Default execution keeps its hot loop.

## Canonical complete transfer measurements

100,000 rows ×105 columns, full typed Arrow IPC, RESP3, local four-CPU/two-GiB
Redis 8.6.2/Search 8.6.0. One warm-up per method/API, five measured rounds for
wide 16 MiB, six exactly balanced rounds for disabled narrow control, three
for other sweeps. Alternating forward/reverse order; same immutable fixture and
output contract in each paired matrix. No task tests or profiles ran during
these canonical matrices. Every value and row multiplicity is independently
checked against source after timing.

| Projection / budget | API | Total seconds, baseline → budget | First batch seconds | Max worker RSS MiB |
|---|---|---:|---:|---:|
| 105 columns /4 MiB | regular | 2.542 → 3.224 | 1.731 → 0.301 | 272.1 → 167.6 |
| 105 columns /4 MiB | partition | 1.758 → 2.033 | 0.948 → 0.434 | 175.3 → 166.5 |
| 105 columns /16 MiB | regular | 2.555 → 2.787 | 1.737 → 0.298 | 269.4 → 166.8 |
| 105 columns /16 MiB | partition | 1.777 → 1.823 | 0.926 → 0.458 | 177.8 → 164.9 |
| 105 columns /64 MiB | regular | 2.562 → 2.775 | 1.715 → 0.337 | 269.9 → 173.2 |
| 105 columns /64 MiB | partition | 1.866 → 1.790 | 0.989 → 0.448 | 174.3 → 170.9 |
| 3 columns /16 MiB | regular | 2.144 → 2.112 | 1.545 → 0.790 | 124.6 → 123.5 |
| 3 columns /16 MiB | partition | 0.874 → 0.915 | 0.518 → 0.508 | 119.6 → 118.5 |
| 105 columns, varied strings /16 MiB | regular | 2.729 → 2.971 | 1.850 → 0.320 | 572.8 → 199.2 |
| 105 columns, varied strings /16 MiB | partition | 2.008 → 2.013 | 1.042 → 0.486 | 249.7 → 195.7 |
| 3 columns, varied strings /16 MiB | regular | 2.399 → 2.450 | 1.703 → 0.428 | 402.4 → 188.0 |
| 3 columns, varied strings /16 MiB | partition | 1.071 → 0.926 | 0.595 → 0.440 | 198.5 → 161.0 |

These are medians, not universal settings. In the primary 16 MiB wide case,
regular RSS falls about 38%, first-batch latency about 83%, while total time
increases about 9%. Partition total time increases about 3%. The smallest budget
has a larger throughput cost; partition gains in individual sweeps do not justify
a universal default. With varied strings, wide regular RSS falls about 65%,
but total time rises about 9%. Keep byte batching off when total speed is the
priority; enable/tune it when earlier delivery or lower batch memory matters.

RSS is the maximum lifetime high-water mark of one worker, including imports,
not simultaneous aggregate worker memory. Each worker normalizes/writes the
same complete IPC result; first-batch sizes intentionally differ under budgets.

The three-column control is `watchid, userid, searchphrase`. The varied-string
fixture changes only SearchPhrase in the official prefix, adding NUL and a
reproducible range of UTF-8 lengths (1–6,375 bytes, mean 1,747.7). It is a derived
sensitivity control, not unmodified ClickBench. See [generator](make_long_strings.py)
and [source identities](varied-strings.json); wide/narrow controls use the same data.

## Disabled-budget controls

| Projection | API | Baseline median (range) | Disabled candidate median (range) |
|---|---|---:|---:|
| 105 columns | regular | 2.529 (2.510–2.542) | 2.574 (2.568–2.577) |
| 105 columns | partition | 1.766 (1.709–1.803) | 1.777 (1.724–1.787) |
| 3 columns, six balanced rounds | regular | 2.505 (2.460–2.525) | 2.516 (2.465–2.561) |
| 3 columns, six balanced rounds | partition | 0.947 (0.800–1.017) | 0.921 (0.886–1.037) |

Ranges overlap; no default speedup is claimed. Earlier exploratory narrow
variation and implementation iterations are retained under [preliminary](preliminary/).
The final balanced narrow repeat is effectively flat.

## Verification and reproduction

All **148 canonical executions** validate complete values/multiplicity: 32
warm-ups and 116 measured trials. Raw matrices include CPU, Redis commands and
both network directions, ranges and individual timings:

- [wide-16m](wide-16m.json).
- [wide-64m](wide-64m.json).
- [wide-4m](wide-4m.json).
- [narrow-16m](narrow-16m.json).
- [wide-disabled](wide-disabled.json).
- [narrow-disabled](narrow-disabled.json).
- [varied-wide-16m](varied-wide-16m.json).
- [varied-narrow-16m](varied-narrow-16m.json).

Correctness covers both APIs, direct and expression paths, exact BIGINTs, NULLs,
NUL/UTF-8/binary payloads, recursive sizing, limits, empty output, huge rows,
invalid/overflowing settings, cancellation and retained Arrow ownership with
checked allocators. Full Go/native and focused race results are recorded in
[validation](../../../docs/sdlc/changes/remaining-performance/validation.json).

Use the existing [transfer runner](../../wide-transfer/README.md), adding
`--candidate-batch-bytes 16777216`; choose 4194304, 67108864 or 0 for other
sweeps. `--candidate-batch-rows` also supports matched row ceilings. Use a
dedicated server; `--load` replaces transferbench.hits. The manifest pins binaries,
source, fixture and environment. [Derived fixture generator](make_long_strings.py)
uses only the copied official prefix; update its local paths for reproduction.
These measurements establish no 100M-row/WAN/TLS or other-architecture speedup.
