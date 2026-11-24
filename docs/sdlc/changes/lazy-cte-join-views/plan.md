# Plan

2026-11-23; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Review the sole production `joinItem.view` caller, in-memory scan behavior,
   base-row emptiness/costing, and lateral/outer/isolated join paths.
2. Capture a failing structural regression before changing `join.go`.
3. Return metadata-only join views and defer the unchanged qualification loop
   to the existing per-item scan, with cleanup of its input owner.
4. Add structural lifetime and pure result regressions; preserve all source
   fields, physical-key alignment and mutable tag isolation.
5. Run focused pure Go tests with no Redis URI, two Go processors and sequential
   package execution. Record review and limitations.
6. Hand the isolated commit to the root agent for integration and an eventual
   Q4-only comparison after baseline and managed Flex page-filter validation.

No live SQL, Redis/container operations, full benchmark suites, push or merge is
part of this implementation task. Human review remains pending.
