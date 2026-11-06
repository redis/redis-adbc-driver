# Explicit ordering with an unavailable aggregate cap

Date: 2026-11-05. Status: focused verification complete; overall parity remains
in progress. See [specification](spec.md), [plan](plan.md).

An authenticated reader may query its tables without CONFIG access. Assuming the
default aggregate cap then makes an explicit ORDER BY fail when the deployment
has a lower cap. This affects both a single immutable baseline and layered row
versions. The existing implicit-order directory shortcut does not address it.

Before selecting the ordered scan path, the planner now checks an unavailable
cap using an empty generated `FT.AGGREGATE … FILTER 0 LIMIT 0 2147483648`
query. This does not evaluate the SQL predicate, projection or sequences. A
direct native `SEARCH_LIMIT_OVER LIMIT exceeds maximum of N` argument refusal
supplies the actual ceiling. Plain, wrapped, joined, transport and unrelated errors
stop planning. The command carries the existing no-retry marker. Unexpected
rows, cursors and warnings fail; any returned cursor is closed.

The existing exact directory/local sort applies the predicate and global
ORDER BY/OFFSET/LIMIT once when a segment exceeds that bound. Single baselines
can use the same directory representation without changing persisted metadata.
Rounded sort-key tie expansion must also fit the segment bound, even when the
SQL LIMIT is small. Required incremental streaming refuses a materializing plan
before evaluating volatile predicates. Under the standard unlimited cap the
ordinary baseline index path remains available. A verified CONFIG value avoids
the extra probe; unavailable CONFIG adds one empty query per eligible ordered
statement. No comparative performance improvement is claimed here.

## Evidence

All live checks use an owned Redis Software 8.8.0 fixture with Search, strict TLS,
noeviction and active/effective 4x QPF, verified through the management API.
Its three shards are behind the Software proxy; this is not Cluster API evidence.

- Before the fix, the native cap-137 run fails on the single-baseline descending
  query with `FT.AGGREGATE failed: SEARCH_LIMIT_OVER LIMIT exceeds maximum of
  137` (case 0.02 s, root package 1.065 s). This is a real server refusal.
- With the fix, the final actual cap-137 selection passes **10 test events**, no
  test skips/failures, root package **2.704 s**. It checks the single baseline,
  layered update/delete visibility, every exact BIGINT/string/NULL value,
  unbounded ordering, global offset/limit, residual filtering and rounded ties.
  Small bounded sorts retain the index path; a native empty-probe case also
  runs under the ordinary server cap without injecting a refusal.
  A sequence proves one evaluation per visible row; required streaming proves
  zero sequence consumption before refusal. The wrapper restores the original
  cap and re-verifies effective 4x QPF, including after the negative run.
- At the ordinary server configuration, the expanded focused race selection
  passes **28 test events**, no test skips/failures, root **2.871 s**. It also
  exercises native-error classification and single-attempt plain, wrapped,
  joined, EOF, permission and canceled-refusal outcomes. No data reducer opens
  and no SQL sequence advances after those failures.
- Neighboring ordinary/snapshot ordering, exact rounded values, NULL ordering,
  late projection and lifecycle races pass **62 test events**, no test
  skips/failures, root **25.851 s**. This run precedes the final two test-only
  additions; its production implementation is unchanged.

The native selection is `TestSnapshotExplicitOrderHiddenAggregateCap` with
`REDIS_TEST_NATIVE_HIDDEN_CAP=137` only after the management API confirms that
actual setting. Without that test-only environment declaration, its separate
reader client injects the same native refusal while using real Software rows,
directories and indexes. The injected path is reported separately from the
actual server-cap run. No fake refusal is used for the latter.

This checkpoint does not prove full current-source native/platform suites,
shared-role authorization, Cluster API/failover/persistence, public process and
input resume, utility staging limits, Flex source snapshots or throughput parity.
