# Pinned late-projection regression fixtures

Date: 2026-11-04. Parent e468176. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Failure and correction

The ordinary diagnostic after concurrent ingestion reaches 68 pass events, two
explicit skips and one failure, stopping at TestSQLLateProjection in 24.496
seconds root package time. Its internal planner reads snapshot metadata without
a pin. The adjacent TestLateProjectionDeletedAndCancelled independently fails
for the same reason before reaching its direct deletion of a managed HASH.

The planner fixture now acquires a real statement pin and retains it through
planning, scan setup, cursor inspection and cleanup. It checks each real segment
cursor when ordered visibility merging is used, retaining the assertion that
wide payloads are excluded from cursor input. All original SQL, case/escape,
large-int ordering, OFFSET/LIMIT and nanosecond timestamp assertions remain.

Missing-HASH refill is tested on a uniquely named, explicitly owned unregistered
HASH/index fixture, never by deleting an immutable managed row. It still deletes
one matched HASH before projection fetch, requires both surviving payloads in
order, checks EOF at the global LIMIT and checks cancellation. The test creates
the real Search schema and cleans up only its exact fixture index/HASHes.

A separate managed regression opens late projection before DELETE and UPDATE,
checks fresh rows, then TRUNCATEs and DROPs the table and runs GC. Only afterwards
does it fetch the original projection: two original row identities, exact values
9007199254740993/9007199254740994, the original string and NULL, with the original
global LIMIT. No published HASH or index is mutated by a test hook.

## Verification and scope

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before database selections.
Mutating suites run sequentially on macOS ARM64; logs and operational identifiers
stay private. No ClickBench run or reload.

The final three-case race selection passes all three events, zero skips/failures,
root package 2.423 seconds. An initial selection omitted REDIS_SNAPSHOT_URI and
skipped the new managed case; the final selection explicitly sets the disposable
endpoint and executes it. Unit packages pass 946 events with 844 expected
integration skip events, zero failures, root package 26.332 seconds. Unit skips
are not database compatibility evidence. Formatting and diff whitespace checks
pass. This checkpoint changes fixtures and coverage, not production code.

The full snapshot/native runs in the [ingest checkpoint](ingest-admission-validation.md)
cover its production revision; the new late-projection regression is separately
executed here, not added to a claimed full-suite pass count. The continued
ordinary diagnostic and full platform/Cluster/recovery, process resume,
sustained compaction and comparative performance gates remain open.

Author review inspected pin/iterator cleanup, real cursor payload inspection,
fixture ownership, exact values/NULLs, global LIMIT and cancellation. This is
not independent maintainer approval.

The continued ordinary selection starts again at TestSQLLateProjection, covering
695 selected top-level names until its first failure. It reaches eight passes,
one explicit profiling skip and one failure at TestSQLLengthMetadata, root
package 4.126 seconds: another internal metadata fixture lacks a statement pin.
Later selected cases did not run. Separate diagnostic counts are not added to
claim a full ordinary suite pass.
