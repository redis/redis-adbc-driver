# Validation status

[Intent](intent.md), [specification](spec.md), [plan](plan.md).
Parent context: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

The real validateSnapshotTables predecessor reproduced joined/wrapped native
query-proof NOPERM triggering replacement CREATE and losing the original unknown
error; later FT.INFO composed NOPERM was mislabeled Unauthorized. Those focused
regression groups failed before correction, then passed after direct assertions.
The experimental corrected source feb325a9 passed 23 relevant verified Software
race tests with zero skips/failures. Complete actual-source evidence and retained
failing attempts are linked in the [experiment archive](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md).

This clean safety-only split is later source from baseline 7b94a93c. The exact
native assertions are the same as that verified experimental source; runtime
batching is omitted. The private transient classifier has the same direct-only
semantics, with additional real-validator tests preventing polling joined/wrapped
unknown inconsistent-state errors. These new split-source tests have not been run
by this agent: the coordinator explicitly reserved the next integration checks.
Earlier experimental timings/tests are not relabeled as this split commit.

Source inspection, gofmt and diff checks pass. Offline audits independently
recompute all 384 instrumented Go observations and all four complete 24-record
native subsets (96 records total), with all 2,304 measured native operation
observations, warmups, complete post-states, hashes and cleanup retained.
Publication batching is explicitly NOT SELECTED: native four-client DELETE p50
regressed 30.3%, despite throughput/p95 gains and gains in other cases. Integrated
code keeps the correctness improvement only. No new performance gain is claimed.

## Final integrated verification

The [combined follow-up validation](../performance-followup-20261114/validation.md)
records final guarded source 3f9cf6a8, integrated race/unit/snapshot outcomes, all 42
final native comparisons and ownership cleanup. Earlier pilot/diagnostic sources
and limits above remain unchanged; they are not relabeled as the integrated build.

Final selected verification and actual measured source are recorded in the
[combined validation](../performance-followup-20261114/validation.md); earlier
scoped source/measurement limits remain unchanged.
