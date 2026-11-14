# Intent: live unmanaged projection retrieval

Date: November 14, 2026 (America/Los_Angeles). Author: Codex. Requestor:
repository user. Base: merged main 0b216478. Status: implemented, focused
validation complete; human review pending. [Spec](spec.md), [plan](plan.md),
[validation](validation.md).

Unmanaged filtered aggregates still read 10K complete HASHes after Search
candidate discovery. The previous 100K/four-column profile attributes most time
to those commands and decoding; the earlier vector optimization did not remove
that cost. Reduce transferred fields while preserving exact source values,
current existence, index membership, predicate rechecking and caller permissions.

Limit this change to filtered unmanaged retrieval. Keep full LOAD-star scans,
managed storage/publication, direct known-key access and application-owned
indexes unchanged. No new product support, optional consistency mode, 100M
ClickBench load/rerun or universal speedup claim is in scope.
