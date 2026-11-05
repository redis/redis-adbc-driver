# Owned compaction index readiness polling

Date: 2026-11-05. Status: correctness checks passed; native performance decision
pending. Parent production: 2be297e, preserved baseline build at d1d2778.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[selective compaction measurements](selective-compaction-validation.md).

The measured selective job spends about two seconds in inactive waits while its
copy/index/install calls total about 100 ms. An owned index returning not-ready
is treated like idle work discovery. The next experiment must distinguish that
known pending work from an empty queue or another worker's ownership, without
changing storage, source identity, leases, readiness/publication proofs or
unknown-operation handling.

Retain the existing five-millisecond pacing after actual progress and one-second
idle discovery. Only a successfully acquired, validated job awaiting index
readiness should use bounded exponential delays from 10 to 100 milliseconds.
Absent explicit wakeups, long waits poll at most ten times per second after the initial backoff;
other workers continue idle discovery. Errors retain lease release and their
existing 100-millisecond retry delay. Publication conflicts and existing writer
yielding keep their current behavior. Cancellation and explicit wakeups must
remain effective. No new option, storage field or protocol version is required.

The worker regression creates a real immutable output/index, injects a busy
FT.INFO observation, and issues no wake between subsequent probes. A single
busy observation must not invoke the one-second idle interval. Repeated busy
observations must back off rather than spin. Cancellation while pending must
join the worker, release ownership, and permit another client to finish the same
durable output. All final rows and exact BIGINT values are checked. The current
ordinary database suite must terminate before this disposable fixture runs.

A separate opt-in foreground profile follows the sustained comparison's two
20,000-row tables, 64 alternating UPDATE RETURNING statements and complete
final-row verification. It observes foreground metadata/visibility/publication
client calls; the background worker has its own unhooked pool. Pipeline time is
counted once with command count, not once per command. Client latency sums can
overlap and are not CPU/exclusive wall-time attribution. The fixture records
categories/counts/timings only, without SQL/row values, keys or endpoints.
It is an instrumented Go experiment, not a native throughput comparison.

Before choosing a production change, capture the worker regression failing on
the old scheduling and collect foreground attribution. Then repeat recovery,
cancellation, index readiness, writer-yield and complete snapshot/native checks.
Finally rerun the unchanged isolated six-process native comparison in both
orders with exact source/library/harness hashes and every result checked.
The existing 61.217x throughput deficit and 1.985x median regression remain
unresolved until new measurements establish otherwise. Reader authorization,
public input/process resume and current platform/Cluster/HA/persistence coverage
retain their separate acceptance gates.


Raw client-call observations: [before](compaction-readiness-before-attribution.json)
and [after](compaction-readiness-after-attribution.json), with exact fixture/source
hashes. These instrumented runs predate the later test-only pin-origin hooks.

## Local experiment results

The original readiness scheduling fails the single-busy-observation regression:
no second probe arrives within 500 ms (the test fails in approximately 0.8 s).
The changed worker passes all three cases in ten race repetitions: 40 passing
test events, root 15.988 s. Recovery, old-owner fencing, writer yielding,
backpressure, close and obsolete-job cases pass 30 race events, root 13.007 s.
Unit-only race checks pass 1,098 events with 964 expected skips, root 54.109 s.
The complete snapshot race selection passes 412 events with 25 explicit skips,
root 440.766 s. These selections include the preserved protocol-15 native worker
handoff cases; absent Cluster and reader-principal gates remain skips.

The versioned native library SHA-256 is
`6650d3ebb4715e5aa0a92834e86ff662321d7c46225fbfa77dcb75c5acf85490`.
Full ADBC validation passes 565 tests, eight skips and three subtests, 88.87 s.
Database runs are sequential on the owned strict-TLS Software 8.8.0 fixture,
three primaries behind its proxy, active/effective 4x QPF and noeviction.
These are local macOS results, not Cluster API or hosted platform passes.

The instrumented paced selective job changes from 2,145.849 ms (2,028.124 ms
waiting) to 1,405.566 ms (1,088.615 ms waiting). Index observations increase
from four steps / 65.519 ms to 15 steps / 265.481 ms. Initial-scan readiness still
requires substantial time; shorter polling adds control and index calls. The
separate instrumented foreground workload's median changes from 66.907 to
68.299 ms, p95 from 2,892.301 to 2,823.107 ms. This does not establish improved
native throughput or performance parity. The unchanged six-process native
comparison remains necessary before retaining this production scheduling change.

Author review checks: the pending flag is returned only after acquisition,
job validation and an index-phase not-ready result; idle discovery, install
conflicts, leases, unknown replies, publication proofs and durable source/output
identity remain unchanged. Cancellation joins and a different client resumes
the same output. More frequent index/control calls are the measured cost. These
checks are not maintainer approval or completion of the overall parity work.
