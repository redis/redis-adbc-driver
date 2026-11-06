# Measured immutable-source validation batching

Date: 2026-11-04. Implementation 885b2bc. Full [parity](plan.md) remains incomplete.
[Raw runs and every sample](validation-batch-benchmark.json).
[Correctness and regression evidence](validation-batch-validation.md).

Four native processes run in before, after, after, before order: pre-batch
ac3c2600509c789641164916742621a13ff1fd99 versus batching revision
885b2bc6ff3ee3ae9fb608d8f7a0d12e4622ee1a. Both have the numeric NULL-order
optimization and protocol 15. Preserved/rebuilt versioned native libraries have
verified hashes recorded in the raw data, along with the exact committed harness
hash. The published revision rebuilt successfully before measurement.

The fixture, tools, dataset, independent complete Arrow-result validation and
sampling method match the [numeric NULL-order comparison](numeric-null-order-performance.md):
macOS ARM64, Go 1.27.3, Python 3.14.10, PyArrow 26.0.1, ADBC manager 1.13.0,
redis-py 8.1.0; owned strict-TLS Redis Software 8.2.0-78.18 / Redis 8.6.2,
three primaries, noeviction, active effective 4x QPF verified per process.
Each process resets its owned dataset, inserts 20,000 rows plus 32 unrelated
tables, updates id=1, deletes id=2, and validates 19,999 visible rows. One warmup
and five retained timed samples per query/process produce ten samples per
revision/query, pooled below. Timing includes execution, complete fetch and
independent tuple validation. All four processes and every result pass. No other
tests or builds ran during measurement. Physical segment counts were not sampled;
background maintenance was enabled. Managed data still shares the metadata slot.
This is a synthetic workload, not ClickBench or sustained write/compaction proof.

| Query | Pre-batch median ms | Batched median ms | Pre-batch/batched |
|---|---:|---:|---:|
| selective, no matching visible row | 13.910 | 12.898 | 1.08x |
| matching selective | 14.228 | 13.773 | 1.03x |
| grouped count/sum | 15.129 | 13.855 | 1.09x |
| distinct count | 16.022 | 13.915 | 1.15x |
| top-K with default NULL order | 16.983 | 16.726 | 1.02x |
| top-K with explicit NULLS LAST | 14.019 | 13.057 | 1.07x |
| direct row-ID point read | 11.461 | 11.183 | 1.02x |

The deterministic transport test proves fewer validation round trips. Runtime
medians improve modestly in this cohort, with small differences potentially
within ordinary measurement variation; no statistical significance or all-workload
speedup is claimed. Batching alone does not explain most remaining read cost.
The earlier mutable-baseline comparison used ac3c260, not this revision, and is
not a paired current-baseline measurement. Broader read/write/storage parity
remains unproved. Profile pin/metadata/planning costs before choosing another
optimization; do not remove readiness or ownership checks based on these timings.

An initial invocation lacked the required snapshot URI environment variable and
exited before setup/timing. Its private error logs were preserved; no timed sample
was discarded. The corrected invocation used the same committed harness. The
completed dataset was cleared only after all native processes were terminal and
fixture ownership/TLS/noeviction/effective QPF were reverified. Formatting, JSON
parsing, exact harness hashes, document links and diff whitespace were checked.
Author review is not independent maintainer approval. Upstream integration,
current full ordinary/native/platform/Cluster recovery, restricted readers,
public process/input resume and sustained compaction remain required.
