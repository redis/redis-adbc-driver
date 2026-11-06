# Arrow storage fixture partition lifetime

Date: 2026-11-03. Parent ec92fe9. Production protocol remains 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduced failure

An isolated ordinary TestArrowAcceptanceStorage run on verified Software 4x QPF
passes fifteen Arrow layout subcases, then fails before the next case can ingest:
DROP TABLE IF EXISTS cannot reserve a generation because sixteen generations
are retained. This is not a decimal256 conversion failure. The old fixture
produces a live partition set for every layout but never releases the group.
Statement Close intentionally preserves descriptors handed to independent workers
for their bounded lifetime, so those groups accumulate across cases. The server
correctly applies backpressure rather than reclaiming their protected roots.

The terminal failing run took 5.656 seconds for the root package. Its case helper
also calls FailNow on a parent testing.T, obscuring the useful generation-limit
error with a subtest runtime.Goexit diagnostic. No assertion or generation limit
has been removed or relaxed.

## Fixture correction

The shared canonical-storage helper retrieves each live set's documented
partition_snapshot_id, registers failure-path cleanup, fully reads and compares
the worker's schema and complete unordered multiset of canonical Arrow rows,
then explicitly executes DROP PARTITION SNAPSHOT. Reading the released descriptor
must fail with the specific expired-or-released diagnostic. Cleanup also releases
the set if a later assertion fails. Helpers report failures on the current
subtest while retaining sequential shared connections.

The independent normalized nested/uint64 storage fixture also releases its live
set after use. Create/append/empty input slices, exact decimal/unsigned/nested
values, metadata reload, streaming and retained IPC replay after source TRUNCATE
retain their existing checks. Production lifecycle and storage code are unchanged.

The first corrected broad selection passes 71 test events, then fails the
oversized-normalization fixture (root package 24.972 seconds). That legacy
fixture expects an empty target table after rejected ingestion. Atomic snapshots
correctly leave no published table. The fixture now checks the same precise
INVALID_ARGUMENT normalization error, an unchanged catalog root for both scalar
and stream bindings, and NOT_FOUND metadata after rejected ingestion. It then
creates the same logical table and verifies an inserted value, proving rejection
did not leave a reservation blocking future work. This strengthens the atomic
failure assertion rather than accepting partial publication.

## Verification

The initial run's fifteen live groups were observed through read-only catalog
inspection after its process exited. Verification waits for their normal bounded
expiry rather than deleting catalog state or revoking unidentified capabilities.
All database-mutating runs remain sequential on the owned strict-TLS Redis
Software fixture, Redis 8.6.2, three primaries, noeviction and active effective
4x QPF verified by make -C go test-software. No ClickBench load or timing run.

Final integration selection exits zero: 72 test pass events (six top-level
tests and their layout cases), zero skips/failures; root package 32.308 seconds.
The selection covers TestArrowAcceptanceStorage, TestArrowNormalizedStorage,
TestArrowResultLayouts, TestKeySourceStreaming, TestArrowLayoutInventoryStorage
and TestArrowLayoutExpansionRejectedBeforeRows, with `-race -count=1 -p 1`.
Other packages have no matching cases and do not add compatibility evidence.
Unit-only packages with Redis endpoints removed pass: 923 pass events, 821
expected integration skips and zero failures. Those skips do not establish
Redis compatibility. Go formatting and diff whitespace checks pass. An initial
sandboxed unit attempt failed because the Go build cache was inaccessible; the
reported successful run used approved cache access.

## Limits and author review

Review covered explicit group ownership, release after complete fetch, early
failure cleanup, duplicate release safety and cleanup before table/connection
teardown, as well as unchanged-root and absent-table checks for rejected input.
Existing snapshot tests independently cover active child readers
surviving group release; this fixture consumes its child before releasing.
This is author review, not maintainer approval.

The restricted reader-role regression and production lease integration remain
open. This focused selection is not a green complete ordinary suite, current
Cluster/failover/persistence proof or evidence of all-workload performance parity.

## Subsequent ordinary diagnostic run

At 875f5f0, a sequential ordinary Software run with `-count=1 -p 1 -failfast`
and command-line `-skip TestACLAppIndexReader` reaches 388 pass events, nine
capability skips and one failure in 110.561 seconds for the root package. The
known ACL test is deliberately excluded to find independent regressions; no
repository skip was added, and this is not complete ordinary-suite success.

The next failure is TestSQLBugIssues: its raw legacy metadata GET is rejected
because catalog access requires a statement snapshot. Its intentional removal
of the folded_identifiers definition flag now uses the existing pinned-read and
guarded private-publication fixture helpers. They preserve physical identity and
publish through the normal protocol, rather than changing an immutable source.
All existing case-folding, quoted identifier, grouping, large LIMIT/OFFSET and
exact variance/type assertions remain. This does not add a storage migration.

The corrected TestSQLBugIssues, TestSQLPaginationConfiguredCaps and
TestSQLBoundStringsOutliveTheirBuffers selection passes with `-race -count=1
-p 1`: three test passes, zero skips/failures; root package 4.324 seconds.
The broader diagnostic run has not been repeated after this follow-up, and the
known restricted-reader ACL failure remains open.
