# Verification and review: UNION ALL streaming

Date: 2026-10-22. Author/reviewer: Codex; agent review, not human approval.
Base: `d4cbccf`. Links: [intent](intent.md), [spec](spec.md), [plan](plan.md),
[#459](https://github.com/alberttwong/redis-adbc-driver/issues/459).

## Regression and design evidence

Before implementation, `TestUnionAllStream` failed because ExecuteQuery returned
a known count of 25,014, demonstrating complete materialization. It now returns
-1 and yields at most 113 rows per batch, with all 25,014 values checked.

The new reader retains the pairwise set-operation tree, rather than flattening
nested UNION ALL. The REAL/BIGINT/DOUBLE regression compares nested rounding
against materialized execution. All coercions operate on native Values before
Arrow conversion; no intermediate C/Arrow round-trip can lose logical types.

Review tightened eligibility to exclude all subqueries, including uncorrelated
IN/subquery caches, and CTE declarations. An otherwise streamable leaf can retain
cardinality-dependent subquery state or evaluate subqueries during scan
preparation. Fallback preserves the established complete executor for these
plans. Records and value-dependent decimal schemas also remain excluded.

Existing `TestSQLStreamResults/materialized` expected unordered UNION ALL to
materialize. Its case now includes final ORDER BY, which still requires the
materialized path. New tests separately require unordered eligible UNION ALL to
stream. No join, DISTINCT, window, sort or aggregate assertion was weakened.

## Checks

- Unit-only Go suite: all three packages passed; Redis-dependent cases skip
  when REDIS_URI is unset.
- `REDIS_URI=redis://127.0.0.1:36379/0 go test -race -count=1 -p 1
  -run '^TestUnionAllStream|^TestSQLStreamResults$' ./`: passed with the final
  subquery/CTE guard, child-transition release and concurrent Next/Cancel checks.
  Worker-active count returns to zero.
- Final native library build passed. Final C-interface tests: 11 passed, comprising the
  three new UNION ALL tests and eight existing IPC regressions. These include
  independent full-value checks, row/byte ceilings, native stream/file IPC,
  cancellation/statement reuse, retained Arrow buffers, and removal of partial
  IPC output on a later child division-by-zero error (SQLSTATE 22012).
- A C cancellation assertion initially expected a PyArrow-specific exception;
  PyArrow maps the C stream cancellation to OSError. The assertion now checks
  that concrete error and cancellation text.
- Fresh-process memory comparison: 36/36 trials, 3,840,000 complete output rows
  independently validated. See [report](../../../../benchmarks/union-stream/2026-10-22/README.md),
  raw results and source/binary manifest. No isolated driver-speed claim.

The initial shared Redis Software container disappeared between the first Go
runs and C validation; the cause was not established. That C run failed with connection
refused (8 failures/3 setup errors), before executing query assertions. The task
created an owned local Compose fixture, `adbc-union459`, on ports 36379/39443;
Redis Software Enterprise 8.6.2, QPF active at 4x, six effective Search workers,
mnp routing and 32 connections were verified before further checks. Fixture
memory is 1 GiB, persistence disabled, noeviction, one shard, RESP3 localhost.
No paid deployment was created. Unique C/benchmark tables are dropped in finally
blocks; Go fixtures use the established SQL test harness.

## Review outcome and deferred candidates

Reviewed correctness/ownership, resource cleanup and scope/evidence under
REVIEW.md. The new composition admits no parallel worker grant. Its owner
registry contains the root reader; cancellation closes the active leaf under
that reader's mutex, preserves retained consumer Arrow buffers, and never
opens an unneeded later child. A per-node LIMIT can bypass later runtime errors;
planning/type errors remain eager, and reached child errors can surface during
fetch. No ordering guarantee exists without ORDER BY.

General aggregates, DISTINCT set operations, joins, windows, global sorting and
CTE/derived-table streaming are explicitly deferred: each needs an operator
state/dependency design and separate measurements before enabling. Existing
aggregate-input streaming and bounded top-K remain intact. No spill, new
query-wide limit, universal constant-RSS promise, CDC or cross-platform runtime
validation is included.

The first broad Go integration run omitted the CI Redis Software fixture
setting `REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed`. Seven retained-results
test groups failed because CONFIG-based policy discovery is restricted on this
server. The database API independently confirmed noeviction, QPF 4x and six
Search workers, then the broad suite was rerun with CI's existing declaration.
No assertion was changed or skipped to hide these failures.

Final configured Go integration command:
`REDIS_URI=redis://127.0.0.1:36379/0 REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed
go test -count=1 -p 1 ./...`: all three packages passed (driver 147.642s,
CLI 0.871s, benchproxy 0.195s), with the final eligibility guard. The nonverbose Go run did not capture a skip count; existing environment-gated
cases are not evidence for other platforms or Redis Cluster compatibility.

Final-binary C validation: 11 passed in 2.49s. The 36 memory trials were
rerun after the final eligibility guard and native rebuild; all 3,840,000 rows
validated. Manifest checksums pin those production files, library and raw
results. Ruff, gofmt, whitespace and relative-link checks passed.

Owned local fixture cleanup: `docker compose -p adbc-union459 -f
go/compose.yaml down` completed; no containers or networks with this Compose
project label remain. The local fixture incurred no paid deployment cost.
