# Specification

Date: 2026-11-24. Status: focused lifecycle correction.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

Register a deferred child-context cancellation and worker wait in the parallel
core. It must run while a callback panic unwinds, before the caller's existing
deferred shared-grant release. Every source iterator/worker must complete its
cleanup first. Preserve the exact original panic value; do not recover, replay
work, alter parent context cancellation, or change normal source-error precedence.
No source scan, result shape, worker ceiling or persisted metadata changes.
