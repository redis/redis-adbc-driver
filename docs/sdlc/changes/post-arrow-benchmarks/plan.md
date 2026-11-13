# Plan

See [intent](intent.md), [spec](spec.md).

1. Build the exact requested feature snapshot; create/verify a private Software
   fixture with make redis-up and effective QPF/transport checks.
2. Pilot all write/create paths for correctness, then run reads, writes and
   creation sequentially without concurrent workload interference.
3. Sanitize observations, audit coverage/arithmetic/validation status, and report
   within-run comparisons with explicit timing/index/consistency limits.
4. Verify appropriate harness tests, retain historical artifacts, remove the
   task-owned server/resources, and publish reviewable evidence.

Steps 1–4 completed; see [validation](validation.md) for measured coverage,
checks, cleanup and review limits.
