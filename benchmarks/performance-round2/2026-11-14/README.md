# Performance investigations — November 14, 2026

[PR #440](https://github.com/redis/redis-adbc-driver/pull/440).

Three parallel source investigations used one exclusively scheduled Redis
Software fixture. Builds and live tests were serialized around timing windows.
The [selected environment](environment.json) records the actual server versions,
resources, TLS and 4× QPF settings. No ClickBench or public Python/C-boundary
matrix was run in this round.

## Selected runtime change

[Native missing-row proofs](../../validated-point-protocol/README.md) retain the
same ZCARD/ZSCORE commands in one pipeline per missing published segment. This
saves one round trip per segment miss: single-segment absence 13→12 requests,
oldest overlay hit 17→15, and three-segment absence 19→16. Found-row and private
score-only paths retain their prior shapes. Four baseline regressions reproduced
unsafe fallback to older data after wrapped/joined Redis Nil; the fix accepts only
the direct native absence reply and rejects uncertain outcomes without replay.

The consecutive frozen-binary A1/B1/B2/A2 diagnostic validated 448 measured queries
plus 112 warmups. Timings and controls were mixed; no general latency gain is
claimed. Its pre-acquired execution transport omits normal per-statement transport
acquisition/registration/finish. See the full report for the exact scope.

## Experiments omitted from runtime

- [Unmanaged field-argument boxing](../../unmanaged-projection/2026-11-14-field-args/report.md)
  reduced allocations, but four-client SQL median latency increased in both
  comparison pairs. Source-read fanout was unchanged. The patch, all observations,
  sparse/all-NULL/wide cases and executable-methodology caveat remain archived.
- [Managed admission attribution](../../managed-writer-tail/report.md) found
  multi-second admission tails dominated by time outside foreground transport
  calls. A target-progress pacing experiment was rejected: throughput decreased
  at every concurrency level and foreground commands per write increased up to
  2.6×. Some maxima improved, while several medians and p95s worsened. Complete
  [comparison evidence](../../managed-admission-pacing/report.md) accompanies the
  archived patch; production pacing remains unchanged.

These are finite diagnostic workloads, not server CPU or saturated-capacity
measurements. Raw direct-client reads provide different guarantees from managed
SQL. Ratios from separate historical matrices must not be composed with these
observations.

## Remaining overlay-read cost

A [controlled native sequence](../../point-valid-sequence/README.md) reproduced
the first validation response wait without SQL planning: approximately 12.6–13.6
ms after three FT.INFO/ZCARD proof pairs, versus approximately 0.26–0.29 ms for
following requests. Pool waits were zero; connection socket-read elapsed tracked
the long wait. SLOWLOG provided no threshold-selected sample, so server execution
time and the cause remain unproven. The next bounded diagnostic should separate
FT.INFO-only from ZCARD-only sequences before proposing a runtime change.

## Review and validation

Root and independent subagent source reviews found no remaining runtime blocker.
The point diagnostic's transport scope was corrected after review, preserving
original labels and identifying post-run annotations. Human approval is not
inferred.

- [Broad unit checks](unit-checks.json): 1,616 pass events and 1,337 expected skips
  with Redis endpoints empty; zero failures.
- [Verified Software race checks](race-checks.json): 66 pass events, zero
  failures/skips. Covers proof safety, corruption/visibility, admission,
  cancellation and uncertain outcomes.
- [Final harness integration](final-harness-checks.json): 17 pass events, zero
  failures/skips; recompiles the final evidence-only harness additions under race.
- [Documentation](docs-checks.json): strict build and verification of 45 selected
  public pages passed. Supplemental experiment links and archive hashes were
  reviewed separately.
- [Cleanup](cleanup-proof.json): owned container/network and generated credentials
  removed; preexisting resources and TLS material preserved. All test processes
  and clients had ended before cleanup.

Counts include parent/subtest events. The records identify their actual source
revisions; subsequent integration changed diagnostic harnesses/documentation,
while [selected runtime hashes](runtime-hashes.json) still match the frozen point candidate. These are
local checks, not a full local cluster suite or a claim that GitHub CI has passed.
