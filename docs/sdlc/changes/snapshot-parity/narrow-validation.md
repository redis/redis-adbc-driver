# Pinned narrow exact-value regression fixture

Date: 2026-11-04. Parent 1b76ecc. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

The ordinary diagnostic after SQL NaN selects 672 top-level names and stops at
TestSQLNarrowExactScan after five pass events, one failure and 1.086 seconds root
package time. Its internal store reads metadata without a statement pin; later
it would delete a published managed HASH to simulate a mutable source race.

The fixture keeps its original public exact int64/NULL, distinct/grouped,
predicate/order/limit and ALTER default/rename assertions. Its internal managed
scan now pins a committed descriptor before opening. After DELETE commits and
fresh SQL counts four rows, GC runs and the old scan must return five unique
original IDs, 9007199254740993/9007199254740992, both int64 extrema and NULL.

The missing-HASH cursor primitive owns a separate uniquely named unregistered
index and HASHes. Its schema uses the real native column definitions and exact
original values. All original cursor checks remain: only unsafe v is fetched
from HASHes, the cursor contains exact-safe id, excludes v and pad, and a matched
HASH deleted before fetch never appears in the four surviving results. Cleanup
removes only the exact owned fixture index and HASHes. Published managed data
and indexes are never directly modified. The two cancellation primitives retain
their original status, one-pipeline and batch-bound assertions.

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before the selection. Mutating
suites run sequentially on macOS ARM64. Logs and operational artifacts stay
private. No ClickBench run or reload. The three-case race selection passes all
three events, zero skips/failures, root package 2.254 seconds. Formatting and
diff whitespace checks pass. This checkpoint changes one regression fixture,
not production behavior or a public option.

The complete production snapshot/native checks remain those of the
[ingest checkpoint](ingest-admission-validation.md), with the subsequent
[late-projection](late-projection-validation.md) and [NaN](nan-validation.md)
fixture selections reported separately. The continued ordinary diagnostic,
full platform/Cluster/recovery matrix, process resume, sustained compaction and
comparative performance gates remain open. No disjoint selections are summed
into a full-suite success claim.

Author review inspected pin/iterator cleanup, current versus pinned visibility,
exact values/NULLs, real cursor payloads, fixture ownership and cancellation.
This is not independent maintainer approval.

The subsequent ordinary selection reaches four passes, one profiling skip and
one failure at TestSQLBoundDecimalDivisionScales, root package 1.283 seconds.
The [bound numeric checkpoint](bound-numeric-validation.md) addresses that
production schema-reconciliation failure while retaining the declared-shape
guard. Later selected tests did not run.
