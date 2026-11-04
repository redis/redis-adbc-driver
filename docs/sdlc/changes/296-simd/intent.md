# Intent: exact integer CPU batch experiment

Date: 2026-11-03 (America/Los_Angeles). Requestor: Albert Wong. Author: Codex.
Source baseline: `36aa28d` (merged #290). Status: implementation and measurement.
Issue: [#296](https://github.com/redis/redis-adbc-driver/issues/296).

Measure whether packing decoded maps into contiguous integers can repay its cost
before changing CPU SQL execution. Preserve the lazy integer DAG, observable
error order, exact reductions, cancellation and Arrow lifetimes.

Deliver an isolated comparison/NULL-mask prototype, independent scalar oracle,
conversion benchmarks, and a disposable Redis profile with full result validation.
A no-go decision is a valid result. SQL integration, arithmetic vectorization,
CUDA changes and experimental flags in release builds are excluded.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
