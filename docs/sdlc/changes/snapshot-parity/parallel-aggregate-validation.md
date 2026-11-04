# Parallel aggregate fixture on immutable generations

Date: 2026-11-04. Parent 58d7491. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

The ordinary diagnostic after bound decimal reconciliation selects 662 top-level
names and stops at TestSQLParallelAggregates: 13 passes, one failure, root package
3.836 seconds. Its initial unscoped metadata read fails before direct seeding
would change published HASHes, sequence metadata and an immutable index.

The complete regression now inserts its original 10003 exact integer/float/group
values through one public SQL INSERT and deletes logical row IDs 2500 and 5001
through public SQL. Independent expectations check 10001 visible rows, 8001
non-NULL integer/float inputs and integer sum 9007199214723489. This preserves
the original partition-boundary hole and NULL input cases while registering real
directories, private indexes and atomic generation publication.

All original serial/four-worker/automatic comparisons remain: filtered and empty
aggregates, grouped output, DISTINCT and integer-expression sums. Twelve further
automatic queries must preserve results and record real tuning observations and
row signals. Four public connections share a two-token budget; every grant is
checked and tokens must return after success. A real queued query is cancelled
and must release both queued and active counts.

Scan-error cleanup uses a pinned private plan naming an absent index rather than
dropping a published index. Both explicit and automatic paths must report an
admitted error, release workers, clear cohort pending work and avoid training on
failed scans. A final full aggregate query verifies published data is unaffected.
No Search metadata or result rows are fabricated.

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before the selection. Mutating
suites run sequentially on macOS ARM64. Raw logs and operational artifacts stay
private. No ClickBench run or reload. The complete TestSQLParallelAggregates
passes under the race detector, zero skips/failures, root package 16.339 seconds.
Formatting and diff whitespace checks pass. This fixture-only checkpoint is not
a workload performance comparison; source preparation now uses atomic SQL.

The production [bound numeric checkpoint](bound-numeric-validation.md) records
the current full snapshot/native checks. The continued ordinary/platform/
Cluster/recovery matrix, process resume, sustained compaction and comparative
performance gates remain open. Separate diagnostic counts are not added into
a green full ordinary suite claim. Author review inspected exact fixture values,
immutable ownership, worker admission/release, tuning, cancellation and private
failure isolation. This is not independent maintainer approval.
