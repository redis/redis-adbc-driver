# Retained operation review

Date: 2026-11-09. Issue: [#335](https://github.com/redis/redis-adbc-driver/issues/335).
Reviewer: Codex (implementation self-review, not human approval).
Implementation: `db7b4324`; delivery: [PR #355](https://github.com/redis/redis-adbc-driver/pull/355).

The correctness/lifecycle pass checked detached ownership, source/base references,
lease takeover and stale-token fences, fixed retention, batch progress, SQL semantic
dependencies, index phases, publication/rebase and terminal outcome cleanup. Explicit
resume never executes a SQL program, external discovery or unread Arrow input.
Source/target changes preserve conflict checking. A native index interruption before
its sealed checkpoint is deliberately abort-only. The extra normal-publication
compactor wake introduced by helper extraction was removed; both production entry
points retain their original single deferred wake. The helper also introduced
a duplicate lease-validation round trip on ordinary writes. Its extra entry-point
check now applies only to restored operations; ordinary writes retain their
original pre-index check and atomic publication fence. A final reply-path audit
found that the reused outcome controller pruned expired pins before returning its
answer. `TestSnapshotPublicationOutcomeLookupIsReadOnly` reproduced that mutation;
outcome now returns before directory validation or pin cleanup. The regression and
existing conflict/publication/rebase tests passed with race detection after the fix.

The ownership/security pass checked physical prefix identities, complete capture
and sequence reservation proofs, bounded records/plans/control charges, atomic plan
TTLs, read-only status, native/ADBC error mapping and reader mutation restrictions.
Plans contain metadata policy/dependencies, not credentials or source row values.
Private input values remain in the existing reference-owned journal. Redis ACLs
continue to gate all underlying commands. No operational artifacts or secrets were
added to the diff. Protocol 17 fences older catalogs, readers and collectors.

The intent/evidence pass checked the native copy/adoption/foreign-refresh APIs,
SQL/default and Arrow semantics, exact types/NULL/binary keys, temporary target
refusal, public status counts and limits. The review caught and corrected missing
foreign-import context on takeover, and C error mapping that would otherwise lose
the outer operation handle. Native and Cluster regressions cover both. One recovery
test's two CREATEs conflicted on shared schema metadata; the test now uses an
independent preexisting SQL target rather than weakening dependency validation.

No unresolved implementation finding was identified in these passes. Known limits
are documented: complete-input preparation only; no session-owned temporary target
detachment; unsafe native index interruption is abort-only; no transactions or
migration; no full-scale performance-parity claim. See [validation](validation.md)
for actual tests, source revisions and evidence boundaries.

Repository inspection found main unprotected and no applicable branch rules at
review time. REVIEW.md is proposed guidance and does not itself install a required
human reviewer. This record does not invent a human approval; final CI/merge state
belongs to the PR.
