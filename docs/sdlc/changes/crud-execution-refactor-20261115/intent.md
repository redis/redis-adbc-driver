# Intent: reduce single-row CRUD execution overhead

Requested by the user on November 15, 2026: implement the proposed performance
refactoring with subagents. The clean baseline is merged main
`3b42245402a2d7cf6f363faf7ac713cd36621f90` (PR #442); its production driver code
matches the previous benchmark's `34f4c829`.

Prior native Linux results showed approximately 2-ms managed/unmanaged known-key
reads versus 0.054-ms redis-py, and managed point mutations around 6 ms versus
about 2.3 ms unmanaged. The objective is to identify avoidable work, select only
correctness-preserving measured improvements, and submit reviewable evidence.
A language rewrite or a universal point-latency improvement is not assumed.

Workstreams investigated current command/native lifecycle costs, unmanaged
known-key field projection, shared metadata resolution, and preparation reuse.
The selected runtime scope is unmanaged known-key row retrieval: reuse the
existing exact live reader instead of fetching unrelated HASH fields. The helper
serves ordinary/streamed reads and DML row matching that reaches the same scan
path; required fields include all DML, RETURNING and residual needs. The write/
publication protocol, mutation semantics, storage format, consistency guarantees
and GC policy remain unchanged. No mutation speedup is presumed.

The shared descriptor routing-hint prototype is **rejected and unapplied**.
Its explicit native screen missed the predeclared 10% one-client point-read gate
in both modes. Its source, measurements, slower cases and corrections remain
archived; its tests/results are not relabeled as final selected-code validation.
Normal GC/lifetime behavior stays unchanged, and preparation is not presented
as a demonstrated win.

Root owns exclusively scheduled Software fixtures, final integration, correctness
checks, matched native/width measurements and cleanup. Final selected-build
validation and measurements must be recorded before completion. No relaxed
consistency mode, mutation/input replay, authority cache, ClickBench rerun or
historical-result relabeling is part of this work. See [spec](spec.md) and
[plan](plan.md).
