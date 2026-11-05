# Implicit snapshot order without redundant reducers

Date: 2026-11-05. Parent: e04a87c. Status: focused validation; comparative
native measurements and complete regression runs remain required.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

An unbounded narrow SELECT without ORDER BY still constructs per-segment ordered
Search reducers and visibility masks to recover implicit insertion order. The
exact directory/candidate merge already produces that ascending row-ID order.
Besides extra work, the reducer can fail under a lowered aggregate cap that a
named principal cannot discover. The prior TLS probe with a real Software cap
of 137 reports `SEARCH_LIMIT_OVER` for such a query.

Extend the existing wide-read shortcut to every projection width for unbounded
implicit order. The normal exact visibility path keeps newest replacements and
tombstones ahead of predicates. Selective predicates retain bounded Search
candidates; unfiltered and residual scans retain authoritative directories.
No SQL replay, new option, storage format, relaxed index validation or changed
explicit ordering/LIMIT path is introduced. This does not resolve arbitrary
explicit sorting under an undiscoverable lowered cap.

A separate read-client hook refuses reducers while leaving the fixture writer,
server and immutable indexes unchanged. The pre-change unfiltered regression
fails in 0.25 s with the same typed cap error. After the change, the complete
300-row baseline plus replacement, tombstone and later insertion returns every
independently calculated row in implicit order. Indexed and residual predicates
are checked separately, including a replacement that no longer matches its old
predicate, exact BIGINT values beyond 2^53 and NULLs. No reducer may execute;
unfiltered/residual paths must read exact member directories. Four focused race
pass events, zero skips/failures, root 2.155 s. An initial post-change observer
looked for the wrong directory command; correcting it preserves the row oracle
and verifies the actual ZRANGE member reads.

A broader race selection passes 41 test events, zero skips/failures, root
13.047 s: implicit/explicit ordering, visibility, index aggregates, candidates,
prefetch, NULL-order proofs/fallbacks, readiness transitions, exact HASH slices
and Substrait visibility. These are scoped checks, not the complete suite.

The public native comparison harness adds a complete implicit-order read over
its existing replacement/deletion fixture. All results are fetched and checked
against independently calculated rows inside each timed sample. No performance
gain is claimed without the matched native comparison.

Owned Redis Software 8.8.0, strict TLS, active/effective 4x QPF, noeviction,
three primaries behind its proxy, macOS ARM64 / Go 1.27.3. Proxy tests do not prove
Cluster API routing. Full source-labeled suites and other outstanding parity
gates remain open; the preceding ordinary/snapshot/native results cover earlier
production source. Author review is not maintainer approval.
