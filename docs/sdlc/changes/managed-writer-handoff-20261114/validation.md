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
- `gofmt`, diff and supplemental evidence/source/hash audits passed. A normal native comparison of the actual final gated build, enabled and verified through the public option, remains required before selection. No final native performance result is claimed here.

Root source review found no runtime blocker, and identified callback-retention
wording that was corrected after measurement. Cardinality bounds do not bound
arbitrary caller-owned context memory. The stop closure can indirectly retain
an active execution context; turn release detaches/nils it. Human review is not
inferred. PR: integration pending. No ClickBench, deployment or upstream issue.
