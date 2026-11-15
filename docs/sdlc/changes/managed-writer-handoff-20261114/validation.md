# Validation and review

Date: 2026-11-14. Baseline `d7c0978f`; frozen measured always-on prototype
`03e04002`. The public default-false gate is a later functional change.
[Intent](intent.md), [contract](spec.md), [plan](plan.md),
[full report](../../../../benchmarks/managed-writer-handoff/report.md),
[source/binary hashes](../../../../benchmarks/managed-writer-handoff/provenance.json),
[checks](../../../../benchmarks/managed-writer-handoff/checks.json).

- Frozen prototype race units: four parent tests passed, zero failures/skips.
- Prototype Software lifecycle/admission checks: 28 pass events; uncoordinated native-gate cancel/unknown-admission checks: three; zero failures/skips.
- Exact frozen-binary 48-write B/C/C/B pilot: 12 trials/576 acknowledgements, complete typed final states, no caller retries. No explicit warmup; p99 equals max at its sample count.
- Exact same binaries, 288-write Bwarm/Cwarm/B0/C0/C1/B1/B2/C2: 24 trials/6,912 acknowledgements, all complete typed final states, zero errors/conflicts/retries/missing admission observations. Measured/warmup records and all distributions retained. Four/eight-writer completion improves, with substantial median and some mean regressions. No general latency claim.
- Both binary hashes and the matched harness remained unchanged across pilot/long measurements. All source changes were paused during timing.
- Read-only cgroup counters outside clocks show zero additional throttled periods in all eight long blocks. They include all server/container/setup activity, not query CPU. All invocations independently verified active/effective Software 4× QPF, six workers, mnp/32, 4 GiB/noeviction, strict TLS/RESP3.
- Actual gated candidate race checks after timing: 21 unit events including default/Get/Set/invalid/16 custom-TLS cases; 36 default/live events including explicit opt-in independent-DB/Close/native-gate/connection-option lifetime; 25 enabled SQL/Arrow admission/cancel/lost-reply/compaction events. Zero failures/skips. Counts include parents/subtests.
- All owned test processes/clients/workers ended and an independent verifier observed an empty fixture before slot release. Root owns final resource cleanup.
- `gofmt`, diff and supplemental evidence/source/hash audits passed. The separately frozen actual gated Linux native build is enabled/GetOption-verified and reported below; it confirms the opt-in tradeoff without relabeling the earlier prototype. Human review remains outstanding.

Root source review found no runtime blocker, and identified callback-retention
wording that was corrected after measurement. Cardinality bounds do not bound
arbitrary caller-owned context memory. The stop closure can indirectly retain
an active execution context; turn release detaches/nils it. Human review is not
inferred. PR: integration pending. No ClickBench, deployment or upstream issue.

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
