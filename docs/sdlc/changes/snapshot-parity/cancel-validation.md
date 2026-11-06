# SQL cancellation fixture atomicity

Date: 2026-11-03. Parent c28d735. Production protocol remains 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction

A sequential ordinary Software diagnostic run with `-json -failfast -count=1
-p 1 -skip TestACLAppIndexReader` reaches 402 pass events, nine capability skips
and two failure events (one CTAS child and its parent), root package 115.665
seconds. The known ACL blocker is excluded only on this diagnostic command line;
no repository skip is added and the run is not a complete suite pass.

The first twelve SQL cancellation workload children pass. CTAS cancellation then
fails because its legacy fixture expects partially written rows in a public
table. The table is correctly absent: the first row batch is private, and a
canceled statement cannot publish that output. Calling the parent's FailNow
from the child also obscures the useful NOT_FOUND diagnostic.

## Correction and retained assertions

The CTAS hook still fires after an actual acknowledged row batch and cancels
the caller context. Before canceling, the fixture verifies its registered
directory count and exact first/last physical row values. It requires exactly
one 256-row batch out of the planned 300,000 rows, rather than accepting a
timer that fires before any output exists.

After the existing CANCELLED/57014 and one-second response-lag checks, the
catalog root must be unchanged and table metadata must be NOT_FOUND. The normal
reference-aware collector must reclaim the private rows, directory, data
ownership fields and any index. Fresh CREATE/INSERT of the same logical name
must succeed and return the exact inserted value. No storage code, replay path,
generation limit or cleanup protocol changes.

Shared-connection subtest helpers now report errors on their own testing.T. The
other CPU-loop, join, aggregate, recursive, update and pre-canceled Redis-command
cases retain their error/latency assertions and the original 50 published rows.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites run sequentially on macOS ARM64. No ClickBench run or reload; operational
artifacts and credentials remain private.

`TestSQLCancel` with `-race -json -count=1 -p 1` exits zero: 15 test pass events
(one parent and fourteen children), zero skips/failures; root package 4.140
seconds. This includes the deterministic CTAS batch, its post-cancel cleanup and
name reuse, and the previously unexecuted pre-canceled-command child.

Unit-only packages with Redis endpoints removed exit zero: 923 pass events,
821 expected integration skips and zero failures. These skips do not establish
server compatibility. Go formatting and diff whitespace checks pass.

The next focused parallel-read diagnostic selection stops at
TestSQLCheckedParallelInputs: it tries to HSET a published source row to move
its row-ID attribute across a worker boundary. The read-only generation fence
correctly refuses; root package 0.949 seconds, one failed test. Its sparse row-ID
and sequence fixtures need registered private construction before planning,
while retaining the original exactness, overflow and partition assertions.
Later cases in that failfast selection did not execute. These are small ordinary
SQL correctness fixtures, not a full-scale ClickBench load or benchmark.

## Review and limits

Author review covers the actual acknowledgement boundary, cancellation timing,
public/private data separation, root identity, cleanup ownership, helper lifetime
and successful reuse. The tests do not imply process/input resumption or broad
performance parity. Full ordinary, restricted-reader ACL, current Cluster and
platform/recovery gates remain open. This is not independent maintainer approval.
