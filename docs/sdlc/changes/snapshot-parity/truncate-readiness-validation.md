# TRUNCATE readiness admission and immutable sealing

Date: 2026-11-04. Parent 1a6b863. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The full ordinary race run at driver/test source 21e8509 is terminal: **680 pass
events, 16 explicit skips, one failure; root 443.091 s**. It stopped at
TestTruncateAttributeReadinessRetry because rangeReadinessHook was marked building
but never activated, so it read actual ready FT.INFO instead of intercepting it.
Later tests did not run. The documentation-only Linux checkpoint does not change
that driver's test source. This is not a complete ordinary pass.

The primitive now uses the established ALTER-ready hook with an explicit waiting
channel. Timeout must retain attrsPending and prove that the target's readiness
was inspected. A subsequent ready result clears pending state. Both phases must
issue zero FT.ALTER commands, preserving the published snapshot index. The
10 ms cancellation context replaces the old 1 ms deadline; assertions still
require the typed timeout and actual interception, rather than treating early
context cancellation as readiness proof.

New native TRUNCATE RESTART IDENTITY cases stage real private empty output,
create its final index once and pause catalog publication at FT.INFO. Building
state and both recognized transient inconsistent-index replies are exercised,
each with successful readiness and cancellation. Before readiness, root,
metadata and exact public BIGINT/NULL rows stay unchanged. Cancellation preserves
the winning root, joins publication and reclaims private index/rows/references;
a fresh explicit TRUNCATE then succeeds. Successful readiness publishes an empty
fresh layout. Both branches accept fresh row ID one and exact NULL values.

One old reader retains the complete original definition and exact value above
2^53 through publication, fresh writes and cleanup. Sources reclaim only after
its release. The original operation must create exactly one index, inspect
readiness and issue zero FT.ALTER commands. No automatic SQL or Search command
replay is introduced. No production, storage protocol or ACL changes.

## Validation and review

Owned strict-TLS Redis Software, noeviction and verified active effective 4x QPF
before testing; mutating selections sequential. Combined primitive, native
TRUNCATE, ALTER waiting and terminal-error selection: **20 race test events
pass, zero test skips/failures; root 10.664 s**. CUDA has no applicable selected
tests and is skipped. Formatting and diff whitespace checks pass.

Reviewed real interception versus an inactive hook, timeout/cancel status,
asynchronous join ordering, unchanged old metadata/precision/NULLs, private and
source ownership, one final index, no published-index mutation and no replay.
Author review is not independent maintainer approval. A continuation after the
failed fixture was started separately; its results cannot be combined with the
failed full run into a full pass. Current-protocol Cluster/role/recovery,
platform/native, public resume, sustained compaction and comparative benchmarks
remain open.

The subsequent 774-name continuation is terminal at Go's default package deadline:
879 pass events, 249 explicit skips, no failed assertion events; root fails at
600.729 s. TestSQLTagExactness began 0.719 s before the timeout. See the
[bounded suite budget checkpoint](full-suite-budget-validation.md); this is not a
TAG assertion failure or a completed ordinary run.
