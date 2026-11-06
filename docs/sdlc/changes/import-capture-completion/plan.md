# Import capture completion plan

Date: 2026-11-06. Source: original PR commit `c40322ef`; current base `617e282a`.
See [intent](intent.md), [specification](spec.md) and [validation](validation.md).

1. Rebase only the PR's capture-completion commit onto current main, excluding
   the already squash-merged parent history.
2. Resolve the snapshot-control composition conflict by retaining both metadata
   publication and import capture proofs. Preserve existing admission checks.
3. Run unit tests, focused import tests with the race detector on a dedicated
   Software database, and capture/admission tests through Software Cluster API.
   Verify active QPF4 and effective workers/routing before Redis tests.
4. Record actual results, skips and review limits; push with an explicit lease
   against the original PR head and retarget #332 to main. Keep it draft.
5. Verify the new PR head, mergeability and replacement CI; remove owned fixtures.

Changed production files: snapshot control, import orchestration, import spool,
new capture protocol, and atomic row-batch admission/consumption. Regression files
cover capture completion/admission and existing import-stream batch behavior.
