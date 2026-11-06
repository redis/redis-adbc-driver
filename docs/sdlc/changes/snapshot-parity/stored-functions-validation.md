# Stored invalid functions and aggregate inspection

Date: 2026-11-04. Parent 36fe006. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproductions and restored fixtures

TestSQLFunctionsStoredBefore fails on the merged head before its invalid-function
assertions: unscoped putView requires a statement snapshot. Its fixture now
creates valid views, pins their definitions and publishes private invalid
definitions through a registered workspace. The pinned bytes and valid query
results remain unchanged while current statements reject the invalid function.
Original normal/empty/dbt/GetSchema diagnostics, information_schema visibility,
explicit replacement and DROP assertions remain intact.

Invalid CHECK/default definitions use the existing guarded private JSON helper,
retaining physical identity and publishing through the snapshot protocol. The
original error messages and explicit repair/INSERT results remain checked. An
older pin retains byte-identical valid metadata and its original one-row input
through both definition edits, repairs and subsequent writes. No published
metadata value is overwritten in place and no SQL is replayed by the driver.

The complete eight-test function family then passes, exposing the next failure
in TestSQLOuterAggregatePushdown: aggregate inspection plans through an unscoped
store. Its helper now holds one pin through planning, predicate construction and
actual index reduction. It still collects outer-level aggregate calls and checks
the original pushdown expectations. Related NaN, rounded numeric/REAL/DOUBLE
grouping and nanosecond timestamp fixtures retain their exact output, mode and
fallback assertions.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified by make -C go test-software before each
mutating selection. Suites run sequentially on macOS ARM64. No ClickBench run,
reload or timing comparison. Raw logs and operational artifacts stay private.

- Initial stored-function focused selection: two race passes, zero skips/failures;
  root package 3.252 seconds.
- Function family plus unmodified aggregate inspection: eight race passes and
  one reproduced failure; root package 14.481 seconds.
- Final function/outer-aggregate/NaN-aggregate/rounded-group/nanosecond selection:
  24 race pass events, zero skips/failures; root package 23.772 seconds.
- Unit packages: 943 pass events, 829 expected integration skips, zero failures.
  Integration skips are not compatibility proof.

The current merged-head ordinary diagnostic (`-json -failfast -count=1 -p 1
-skip TestACLAppIndexReader`) reaches 652 pass events, 14 explicit skips and
one failure; root package 289.127 seconds. It passes the restored function and
outer-aggregate cases, then fails TestSQLGroupingSetsPushdown's unscoped planning
helper. Later cases did not run. Skips include profiling gates and Software ACL
administration limits; the sole command-line ACL exclusion is unchanged.

The grouping-set helper now retains one pin through all per-set planning and
index reductions. Its existing per-set predictions and boolean pushdown results
are unchanged. The complete grouping-set/function family passes afterward:
six race passes, zero skips/failures; root package 9.015 seconds. Production
behavior and storage format are unchanged. Formatting, evidence links and diff
whitespace checks pass. Full ordinary success is not claimed.

A subsequent diagnostic selects only the 772 top-level tests after
TestSQLGroupingSetsPushdown in Go's test listing, retaining their order. It
reaches three passes and one failure, zero skips, root package 2.820 seconds:
TestHashInspectCopyBlockers fails in unscoped legacy lastNames setup. Later
selected cases did not run. This selection avoids repeating the inspected
prefix; it is not a complete suite and its counts are not added to the earlier
run to claim full success. The copy checklist's legacy name-prediction behavior
needs separate regression coverage against current private generation allocation.

Hosted [Test run 41795366295](https://github.com/redis/redis-adbc-driver/actions/runs/41795366295)
and [Release binaries run 41795366244](https://github.com/redis/redis-adbc-driver/actions/runs/41795366244)
test the earlier published 36fe006 head and were confirmed live during this
checkpoint. Helpers/coverage and distribution checks passed; Linux Software and
platform build jobs remained in progress at inspection. The
[DuckDB extension run](https://github.com/redis/redis-adbc-driver/actions/runs/41795366038)
completed successfully. These statuses neither validate the current uncommitted
fixtures nor establish complete platform or atomic snapshot parity.

## Author review and remaining work

Reviewed private metadata registration/publication, immutable pinned definitions
and source rows, exact invalid-function errors and repairs, pin scope through
actual reducers, precision/NaN/NULL behavior and unchanged pushdown expectations.
This is not independent maintainer approval. Full ordinary, restricted reader
ACL, current Cluster/platform/recovery, explicit process resume, sustained
compaction and broader comparative performance remain open.
