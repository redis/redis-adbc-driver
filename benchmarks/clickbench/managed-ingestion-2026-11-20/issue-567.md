# Bounded Parquet reader measurement: issue #567 closeout

Measured 2026-11-20 local time. Issue: [#567](https://github.com/redis/redis-adbc-driver/issues/567).
[Shared provenance](method.json) · [Observations](reader-controls.json) ·
[Memory samples](reader-memory.csv) · [Native component observations](native-memory-components.json).

## Complete original-source reader controls

The bounded policy consumed **99,997,497 rows, all 105 columns, all 226 original
Parquet row groups**, separately with one and four readers. Both completed with
exact declared coverage and checked canonical conversions; neither wrote Redis
or loaded the Redis Go driver. These are source-component controls, not the full
managed ingestion required by #566. A second pass confirms the initial complete
source observations with precise maximum-worker-finish timing rather than
one-second progress-poll completion latency.

One current group per worker, immutable shared metadata, `pre_buffer=False`,
`memory_map=False`, `buffer_size=0`, decoding threads disabled, batch size 4096,
no producer queue and no per-batch GC/allocator purge. Fresh process each run,
4GiB client cgroup/4CPU limit. Original source/cache state was not globally
flushed; do not interpret these as cold/remote storage throughput. Full-source
precise timings below are one observation per reader count, not n3 medians.

| Readers | Worker-finish seconds | Process CPU seconds | Process peak RSS MiB | Arrow pool peak MiB | Active-window RSS quarter medians MiB |
| --- | ---: | ---: | ---: | ---: | --- |
| 1 | 48.657 | 47.045 | 426.2 | 196.6 | 277.6 / 294.1 / 294.7 / 285.7 |
| 4 | 19.973 | 61.322 | 1183.4 | 780.7 | 797.9 / 861.5 / 829.6 / 780.9 |

RSS rises during buffer warm-up and varies with group contents, but later
quarters do not grow with cumulative source rows in these complete passes.
Four readers use more memory, as expected. This is an observed plateau on this
file/configuration, not a universal hard RSS bound for different pages/rows.
All 226 groups include uneven worker totals and the final partial batch.

The CSV samples are 0.25s apart, relative to the first resource sample (including
setup); worker-finish time is separately recorded. Charged cgroup file bytes
include source/library cache, with a large inactive/reclaimable component. They
are separate from process RSS and Arrow allocations. Per-component peaks may
occur at different times and must not be added. Source controls have no Go
runtime to measure; Go heap is marked not applicable, never inferred from
RSS-minus-Arrow or called zero driver memory.

## Safe read-ahead/lifetime tradeoff

The old-style control keeps one file reader per worker with `pre_buffer=True`;
the bounded control uses one group-scoped reader with `False`. The selected
16-group source prefix contains 8,091,470 rows and about
1,182MiB compressed data. All selected compressed chunks were explicitly
prewarmed with bounded reads before timing. Both policies process the same
rows/types, no queue/purge, same options/caps, fresh process per observation;
three alternating observations for each policy/reader count. Entire old-policy
226-group prefetch was deliberately not attempted under a 4GiB client limit.

| Readers | Scope / pre_buffer | Median seconds | Maximum RSS MiB, n3 | Maximum Arrow pool MiB, n3 |
| --- | --- | ---: | ---: | ---: |
| 1 | one file / True | 3.929 | 1431.0 | 1224.0 |
| 1 | one group / False | 3.939 | 323.1 | 144.5 |
| 4 | one file / True | 1.642 | 1712.0 | 1344.2 |
| 4 | one group / False | 1.652 | 882.0 | 484.9 |

The warmed-prefix timing ranges overlap; these observations show substantial
memory reduction without a material timing change in this local control. They
do not establish remote/cold-storage tradeoffs. A preceding bounded four-group
control showed little four-reader policy difference when each reader owned only
one group, while the one-reader file lifetime retained more input. Scope plus
read-ahead changes are measured together; no claim isolates a single flag's
causal share. Prefix selection/byte bounds and independent full-source passes
avoid an unsafe all-file-prefetch or intentional OOM experiment.

## Native Go, Arrow and cache accounting

The [twelve native streamed controls](cshared-controls.json) validate every value
and multiplicity in 262,144 rows ×105 columns, serial and four-writer,
unchanged/current; all use the bounded source policy. Separate native component
JSON records process RSS/Arrow peaks through ACK and Go GC marked-live peaks
across the whole case, under an explicit 1GiB Go soft memory limit. The
[representative Go trace](native-go-heap.csv) retains runtime-relative GC samples
through load, maintenance, validation and reclamation; it is not exact phase
alignment or total Go RSS. Validation includes its own bounded DuckDB memory.

Original large-group source-only results do not replace native integration;
rewritten 4K-group native controls do not replace original-group memory evidence.
Together with the [type/lifetime/native checks](../../../docs/sdlc/changes/clickbench-bounded-parquet-567/plan.md),
these establish the measured source policy and keep remaining attribution limits
explicit. Exact native Arrow physical types, required values, BIGINT precision,
row-group transitions, partial batches and exported-buffer lifetime are covered.
Source/schema fingerprints are preserved. Historical client RSS growth is not
retroactively labeled a proved driver Arrow leak.

## Acceptance boundary

Issue #567's bounded-source/memory/lifetime/tradeoff criteria are supported at the
scopes above. No full native 100M run is imposed as a new criterion on this issue.
The full managed 100M load, its combined-client peak, server capacity, backup and
full-scale throughput remain separate #566 evidence gaps. Remote read-ahead and
other files need their own measurements. No resource alarm, replay, AWS action
or global cache purge was used for these reader observations.
