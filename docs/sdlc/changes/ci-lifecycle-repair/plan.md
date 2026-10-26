# Plan and evidence

2026-10-22. Author: Codex. Base: `4e26896`. Verification running.
See [intent](intent.md), [spec](spec.md).

1. Inspect latest Test/Release logs and reproduce the same failing tests.
2. Extract the minimal lifecycle repairs from the ongoing ADBC completion work,
   preserving its independent feature changes and worktree.
3. Verify regressions, race-detected cancellation, full Go suite and native
   validation against an isolated fixture; run hosted Test and Release checks.
4. Review lifecycle, ownership, diagnostics and actual evidence under REVIEW.md.

The isolated fixture setup verified Redis Software 8.6.2, one shard, active
4x QPF. Before the repair, the focused suite reproduced rewrite close, rekey
close, socket diagnostics, stream and UNION ALL cursor cleanup failures.
After the repair, the same tests plus the expanded transport fault matrix
passed in 8.867s, with no skipped or weakened assertions in this selection.
Final verification results are pending.
