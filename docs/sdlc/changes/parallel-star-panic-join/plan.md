# Plan

Date: 2026-11-24. Status: red/green regression captured.
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Hold a real mocked native peer inside canceled-read cleanup.
2. Panic from the actual parallel coordinator visitor and recover in its caller.
3. Reproduce premature panic propagation and token release before peer exit.
4. Add deferred child cancellation plus worker join, without panic recovery.
5. Verify the grant stays held behind the cleanup gate, the same panic reaches
   the caller afterward, no tokens leak, and the parent context stays active.
6. Prepare a clean public delta against the parallel-streaming prerequisite;
   no live SQL, library build or container operation is part of this fix.
