# Plan

Date: 2026-11-20. Source base: `27e5a8d2`. Requestor: repository user. Author: Codex.
Issue: [#554](https://github.com/redis/redis-adbc-driver/issues/554). Status: implemented; focused validation passed.

1. Add live collection iterator and wire it into the existing scan/stream reader; retain materialization for blocking plans.
2. Admit eligible live SELECTs under streaming-required preflight and retain definition checks on buffered batches.
3. Add deterministic discovery integration cases proving early batches, full fixed-source results, binary keys, duplicates, empty pages, residuals, pagination, byte budgets and lifecycle failures.
4. Run focused and surrounding tests on dedicated Redis Software with verified 4x QPF; review ownership/lifecycle and document precise limits.

See [intent](intent.md), [spec](spec.md) and [validation](validation.md).
