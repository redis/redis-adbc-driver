# Verification

Date: 2026-11-24. Status: red/green regression captured; standalone checks pending.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Before the fix, `TestParallelStarConsumerPanicJoinsBeforeGrantRelease` failed:
the coordinator panic escaped and the worker-budget active count became zero
while a peer remained gated in canceled native-read cleanup. The caller's parent
context was still active, isolating the bug to incomplete panic unwinding.

The fix registers deferred child cancellation and worker wait. The test recovers
the same panic in the caller, requires the two-token grant to remain active while
the native peer's cleanup gate is closed, then opens the gate and verifies peer
exit and token release. The gate's short negative wait asserts lifecycle ordering,
not query speed. Source transport is mocked; no endpoint is contacted.

Expanded ownership/parallel regression race tests passed with the guard in place
(5.064s); standalone public-branch checks are recorded after extraction. No live
query, native library build, Redis/container operation or benchmark was run.
