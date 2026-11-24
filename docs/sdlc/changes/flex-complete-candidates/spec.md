# Specification and visibility proof

Date: 2026-11-23. Base: `07b24edb`. Status: local candidate.
See [intent](intent.md), [plan](plan.md).

Use the prior conservative pinned/immutable/managed/plain Flex eligibility gate.
COUNT each source with plain FT.SEARCH LIMIT 0 0, bracketed by fresh complete
index identity/readiness proofs. Reject warnings, malformed counts, and counts
exceeding published physical rows. Admit at most 65,536 source candidates total
and 16 MiB estimated canonical key payload across every segment. Count is a
planning upper bound, never a visible row count. A broad source falls back before
exposing rows to the original exact-directory scan with no page filter.

Fetch every admitted source's matches using one plain unsorted LIMIT 0 n reply.
Require total == prior count == returned keys; reject partial warnings, duplicates,
foreign prefixes, noncanonical/nonpositive/overflowing IDs. Locally pad/sort IDs
as exact int64s. Prove every candidate belongs to its own exact source directory
with ZMSCORE score zero and verify complete directory counts. Apply worker exact
int64 bounds locally only after source-membership verification.

Reuse existing candidate merge with all heads prefetched and done: every visible
match has a candidate in its authoritative winning source; merging identical IDs
and probing all newer directories chooses that newest version. A newer matching
candidate wins; a newer nonmatching version or tombstone suppresses an older
match. Shadowing versions must have valid authoritative HASH identity even when
they failed the source predicate. Selected winners still fetch authoritative
HASH values, validate row IDs, discard tombstones, and evaluate the complete
original residual. Search never defines own/newer directory membership.

Compared with the RAM candidate path, own-source membership and zero-score
validation are stronger. Both candidate paths inspect HASHes of actual candidates
and any shadowing newer version, rather than every unrelated nonmatching row's
HASH. Full source directory cardinalities and pinned published metadata remain
verified. This distinction from candidate1's full-directory control reads is
intentional and follows the existing candidate-read contract; no unrelated
nonmatch integrity claim is made.

Check the lease before preparation, after preparation, and before each visibility
page. Preserve pipeline and individual errors. Only a direct native result LIMIT
refusal with confirmed surrounding index proofs permits capability fallback;
wrapped/joined errors, ACL, transport, stale identity, or partial replies fail
closed. No native snapshotIndexable guard, metadata layout, public option, SQL
source, or field mapping changes.

Reviewable Flex-only implementation: [PR #604](https://github.com/redis/redis-adbc-driver/pull/604).
