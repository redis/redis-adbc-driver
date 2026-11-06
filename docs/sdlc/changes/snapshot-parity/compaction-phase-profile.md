# Compaction phase attribution

Date: 2026-11-05. Status: owned-fixture attribution complete; sustained
performance parity remains incomplete. Parent production: 534cd6f;
current test-fixture correction: c4698f3. See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and the [failed sustained parity gate](atomic-compaction-copy.md).

The six-run comparison improves throughput only 1.095x. Before the next
performance change, separate copy work, repeated control/catalog operations,
index readiness/installation and the worker's pacing using a reproducible
opt-in test. The profiler must retain complete fetch and exact value validation;
it must not log physical names, endpoints, credentials or row contents.

`TestSnapshotCompactionPhaseProfile` runs only when
`REDIS_SNAPSHOT_COMPACTION_PROFILE` is set. It creates a 20,000-row target and
20,000 unrelated rows, performs three exact BIGINT increments, then drives the
real bounded compaction step implementation to completion. Two cases use the
worker's current active 5 ms / inactive 1 s delays, or omit only the active delay.
Each step retains its production GC, ownership, current-generation validation,
source scan, atomic copy, indexing and installation behavior. A separate durable
phase observation occurs outside the step timer. This is attribution under a
controlled fixture, not a replacement for the public native sustained benchmark.
The ordinary production worker additionally runs its orphan-index check; the
profile does not model that periodic check or concurrent foreground writes.

A client hook aggregates round-trip counts/times by command category and phase,
including separate copy commits and control operations. A pipeline contributes
one elapsed round trip with its command count; it is not treated as individual
serial round trips. These are client-observed latencies, not Redis CPU profiles.
Hook activity can overlap heartbeat work, so sums are not asserted to equal wall
time. Explicit wait time and total elapsed time are reported separately. Final
validation fetches every target row and all three columns, including the exact
integer beyond 2^53 and each payload. Unrelated count/range is also checked.

The profiler compiles and skips by default with Redis endpoints unset (root
0.849 s). This verifies only its default gate. No timing result or improvement
is inferred until exclusive owned Software execution with verified active/
effective 4x QPF and noeviction completes. Run database suites sequentially.

## Hosted snapshot coverage gap

The existing hosted Test matrix runs ordinary Go tests without
`REDIS_SNAPSHOT_URI`; its Software job labels do not imply execution of the
snapshot protocol fixtures or Cluster API recovery orchestration. Add an explicit
sequential `make test-software TEST_ARGS="-race -run Snapshot"` step after
ordinary Go tests on each Software architecture, using only the owned fixture
URI. The target reverifies active/effective 4x QPF and noeviction before testing. Its teardown
finishes before native validation. Keep ordinary failures visible, including the
existing SQL reader-authorization work; this step does not enable GRANT, create
ACLs or infer missing Cluster/HA/persistence coverage. A dispatched run predating
this workflow edit cannot count as evidence for the new step.

## Measured result

After the full ordinary process exited successfully, both cases passed with
complete target-row verification: three test events, zero skips/failures; root
21.015 s (case elapsed including setup/final verification: 10.56 s and 8.65 s).
The owned Software fixture reverifies strict TLS, active/effective 4x QPF and
noeviction before execution: Redis 8.8.0, three primary shards behind its proxy,
macOS ARM64 and Go 1.27.3. Production compaction is unchanged from 534cd6f; the
fixture correction is c4698f3. The test file hash and full aggregate measurements
are in [the raw profile](compaction-phase-profile.json).

| Controlled case | Total job ms | Explicit waits ms | Copy steps / ms | Index steps / ms | Install steps / ms |
|---|---:|---:|---:|---:|---:|
| Normal active/inactive pacing | 7,384.717 | 3,753.761 | 314 / 3,354.679 | 4 / 77.481 | 1 / 32.858 |
| Omit active 5 ms delay | 5,501.716 | 2,001.960 | 314 / 3,239.861 | 4 / 74.770 | 1 / 27.384 |

Both cases commit 313 image batches for 20,000 rows. In the paced copy phase,
942 mixed pipelines cost 537.536 ms, 1,884 ZRANGE calls cost 473.261 ms,
313 atomic copy commits cost 334.798 ms, 1,252 ZLEXCOUNT calls cost 302.559 ms
and 313 full-image read pipelines cost 233.786 ms. These client observations
show that copy commits alone account for only a fraction of copy-step time.
The inactive waits total approximately two seconds in both cases. Removing
active pacing saves about 1.88 seconds here, but still leaves a 5.50-second job.
It does not establish end-to-end write throughput or explain every native tail.

This evidence supports evaluating [selective compaction](selective-compaction-design.md)
to avoid repeatedly processing a large unchanged base, while preserving tombstones
in partial-range output. Index/readiness wait reduction remains a separate target.
No source-selection change is enabled by this profile, and no performance-parity
claim follows from it.

The existing required Software SQL SELECT grant test currently fails. Its step
now retains its failing outcome while allowing independent native/platform
checks to run; a final `always()` gate explicitly fails the job if that original
outcome is failure. This does not skip the authorization test, change its
assertions, grant permissions or permit a green job after its failure. The
completed historical run still counts as failed, and the new workflow requires
actual hosted execution before validation can be claimed.
