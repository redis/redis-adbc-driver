# Private index readiness before ALTER publication

Date: 2026-11-04. Parent 2782633. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and production correction

The original ALTER waiter fixture fails in unscoped metadata planning before
checking readiness. Its six existing build/convergence and cancellation cases
now stage a private ALTER layout and exercise actual snapshot sealing/publication,
instead of modifying a published index with FT.ALTER.

This restored fixture exposes a production gap: both recognized Software schema
convergence replies cause validateSnapshot to return immediately. The ordinary
building/cancellation cases pass, while four convergence cases and their parent
fail (two pass events, five failure events; root package 2.376 seconds).

Publication now polls those exact typed Redis server convergence replies within
the existing cancellation/deadline and 10 ms readiness wait. It still requires
the real final index's settled indexing state, document count, prefix and zero
index failures before publishing. It never repeats SQL, row output or FT.CREATE.
Genuine server NOPERM validation errors are terminal Unauthorized errors;
previously they were wrapped as generic I/O. Missing indexes, other server errors
and transport errors remain terminal. A plain transport error containing the
same convergence words is not polled.

## Fixture guarantees

All six original variants remain. While the new index is held unready, the
committed root, current SQL schema and exact BIGINT value beyond 2^53 remain
unchanged. Success publishes the new default only after real readiness. Cancel
leaves the root unchanged, reclaims the failed private index/directory/ownership
and permits an explicit fresh ALTER with a different default and canonical field.
Each original operation creates exactly one index and issues zero FT.ALTERs.

An older pin retains byte-identical metadata, the exact original row and the
original index schema through publication/cancellation and explicit GC. Releasing
that pin allows reclamation. Four additional terminal-error variants require
one readiness request and one index creation, unchanged publication state,
normal private cleanup and intact original SQL rows. These replies are injected
at the real publication boundary; they do not prove actual restricted-role ACL
administration or distributed failover.

The range-readiness fixture now inspects metadata on a pin with hooks installed
before sharing the client. It still refuses inconsistent schemas immediately
and preserves permission/missing-index terminal statuses.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified before mutating suites. Tests run
sequentially on macOS ARM64. Raw logs, credentials and operational artifacts
remain private. No ClickBench run/reload or elapsed-time performance comparison.

Final ALTER/publication/range readiness selection: 17 race pass events, zero
skips/failures; root package 5.614 seconds. Unit packages: 945 pass events,
836 expected integration skips, zero failures. Integration skips and parent
passes whose children skip are not database compatibility evidence. The versioned
native build completes with make -C go build.

The full snapshot race selection (`^TestSnapshot`, `-race -count=1 -p 1`)
exits zero: 244 pass events, 21 explicit skips, zero failures; root package
300.619 seconds. Skips are nine Cluster API cases, six permission cases and
six separately gated reader-model cases, not compatibility passes. Rebuilt-native
C/ADBC snapshot validation passes all 14 tests in 13.674 seconds with zero
skips/failures. All database-mutating selections run sequentially. Formatting,
local evidence links and diff whitespace checks pass.

The subsequent ordered diagnostic selects the 757 top-level names after
TestAlterWaitsBeforeMetadataCommit in the earlier test listing. It passes the
five range-readiness events, then fails TestPendingCleanupUsesCompleteHashInput
in unscoped sharedScanPlan setup: five passes, zero skips, one failure; root
package 1.215 seconds. Later selected cases did not run. This selection is not
full ordinary-suite proof and is not added to earlier prefix counts to claim
success. Pending-cleanup fallback fixtures remain the next regression blocker.

## Author review and remaining work

Reviewed typed-error matching, cancellation bounds, unchanged readiness proofs,
one-time index creation, private rollback, explicit fresh SQL, exact pinned rows/
schema/index attributes and reference-aware reclamation. Storage format and ABI
are unchanged. This is not independent maintainer approval. Full ordinary,
restricted reader ACL, current Cluster/platform/recovery, explicit process resume,
sustained compaction and broader comparative performance remain open.
