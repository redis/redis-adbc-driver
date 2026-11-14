# Validation in progress

[Intent](intent.md), [spec](spec.md), [plan](plan.md).
[PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Scoped pilot/diagnostic evidence retains its actual source and limits in
[point root prefetch](../../../../benchmarks/point-root-prefetch/2026-11-14/report.md),
[unmanaged projection](../../../../benchmarks/unmanaged-projection/2026-11-14/report.md)
and [unselected managed experiment](../../../../benchmarks/managed-publication-readiness/2026-11-14/report.md).
The managed batching experiment is not integrated. Direct native publication
refusal safety guards remain, with real-validator regressions.

Final integrated unit/race/snapshot checks and the complete native comparison are
pending at this production freeze. Do not relabel scoped measurements as this
combined source or infer maintainer approval. Final report will retain every
trial, regression, actual source, build/harness hash, skip and cleanup proof.
