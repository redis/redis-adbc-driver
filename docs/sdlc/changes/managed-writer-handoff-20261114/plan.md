# Managed writer handoff: implementation and verification plan

Date/source/requestor: [intent](intent.md); contract: [specification](spec.md).
Status: opt-in measured candidate; separate Linux native confirmation complete, pending human review.

1. Reviewed connection lifecycle, snapshot pin cleanup, server admission and the rejected target-progress pacing experiment. Complete.
2. Root reviewed opaque identity, bounded registry, cancellation and native fallback. Complete; no runtime blocker found. This is not human approval.
3. Implemented the candidate and focused race coverage for FIFO/handoff, isolation, independent DB handles, cancellation/Close, unknown native outcomes and bounded overflow. Callback retention is documented accurately; cardinality does not bound caller-owned memory.
4. Initial race units passed four parent tests; Software race safety passed 28 parent/subtest events; uncoordinated native-gate cancellation/unknown-admission checks passed three events. All had zero failures/skips. Expanded 16-case TLS bypass coverage was added only after timing and is tracked separately.
5. Froze both persistent binaries, matching profile harnesses and source hashes before all comparisons. The 48-write B/C/C/B pilot validated all 576 writes. The 288-write Bwarm/Cwarm/B0/C0/C1/B1/B2/C2 comparison validated all 24 trials/6,912 writes. Full typed final states, warmups, every observation, throughput, mean/p50/p95/p99/max and command counts are retained in the [report](../../../../benchmarks/managed-writer-handoff/report.md). No SQL/input retries.
6. The long run improves four/eight-writer workload completion and high percentiles while substantially increasing medians and several per-operation means. One-writer throughput is essentially unchanged. No general latency/default-speedup claim is made. The separately frozen Linux native comparison below now confirms the opt-in tradeoff with raw controls, using its actual source labels; human review remains outstanding.
7. Every live invocation verified active/effective Redis Software 4× QPF, six workers, mnp/32, noeviction, strict TLS/RESP3. Cgroup counters outside trial clocks show zero additional throttled periods in all eight long blocks; they are not query CPU. All test processes/clients ended, with independent empty-fixture verification before slot release. Root owns final cleanup.
8. Exact executed source patch and hashes remain preserved. The explicit default-false public gate is a functional post-measurement change, accompanied by a comment clarification and expanded TLS tests. Actual gated checks passed: 21 race unit events, 36 default/live events including explicit queue lifecycle checks, and 25 enabled SQL/Arrow events; all zero failures/skips. These do not relabel the preceding prototype performance. Final native builds/checks use actual new source labels.

[Evidence and provenance](../../../../benchmarks/managed-writer-handoff/provenance.json),
[checks](../../../../benchmarks/managed-writer-handoff/checks.json).
PR: integration pending. No upstream issue, ClickBench or external deployment.

## Explicit opt-in gate added after measurement

The public database option `adbc.redis.writer_admission_queue` accepts exact
`true`/`false` strings and defaults to `false`. It affects newly opened
connections; existing stores retain their selection. Default false avoids
identity fingerprinting, the extra trust-bundle read and local registry work.
True opts eligible managed writers into the bounded process-local tradeoff; it
is not another driver/table mode, a server-admission bypass or a global FIFO
guarantee. Live unmanaged writes are unaffected.

The always-on `03e04002` prototype supplied the recorded Go pilot/long timings.
Adding this public option is a functional post-measurement change. Its actual
default/enabled option, lifecycle and safety checks are recorded separately, and
a new frozen native candidate must enable/verify the public option before any
final selection claim. The preceding Go measurements must not be relabeled as
timings of the final gated build.

## Separate final gated Linux native confirmation

The [frozen Linux/arm64 Python/ADBC ABBA report](../../../../benchmarks/managed-writer-handoff/native-linux/README.md)
uses actual baseline `d7c0978f` and candidate `0884e3c8`, with the public option
explicitly enabled and GetOption-verified on every candidate driver connection.
All 192 records/6,144 operations passed complete original typed/final-state
oracles. Independent offline audits verify exact grids, counts, markers,
statistics, source/library/wrapper identities and cleanup; raw bytes are retained.
Four-writer completion throughput/p95 improve, while medians and means worsen
substantially. All slower cases and raw control drift remain visible. The option
stays default false; no general/default speedup or global FIFO is claimed.

The macOS Go prototype observations remain historical and separate. A Linux
control did not reproduce the macOS post-proof response delay, so no general
Software/driver cause is established. The native data has its own topology and
source/binary labels. Raw automatic_retries labels are unchanged; post-run notes
separate caller intent, verified zero redis-py command retry budget and uncounted
connection management/reconnect behavior. Object setup is excluded, but lazy
first-operation network/execution-pool work remains included. New clients per
trial mean warmups do not prewarm later clients. No timing code changed after
freezing. Human approval is not inferred.

Review: [PR #441](https://github.com/redis/redis-adbc-driver/pull/441).
