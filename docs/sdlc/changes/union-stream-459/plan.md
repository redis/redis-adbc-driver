# Plan: UNION ALL streaming

Date: 2026-10-22. Author: Codex. Base: `d4cbccf`. Status: implemented and verified; awaiting review.
See [intent](intent.md), [spec](spec.md), [#197](https://github.com/redis/redis-adbc-driver/issues/197).

1. Capture a failing large UNION ALL query that returns a fully known count.
2. Add a sequential Value-row source tree and reuse streamReader for batching,
   ownership, cancellation, error delivery and final metadata validation.
3. Verify exact comparison with materialized execution, nested coercion,
   parameters, NULLs, limits, fallback plans, late errors and child cleanup.
4. Build the native library; test the C boundary, including native IPC.
5. Run fresh-process measurements with fixed output batches on Redis Software
   Enterprise with effective QPF 4x verified; independently validate every row.
6. Update the capability matrix and record explicit deferrals for blocking plans.
7. Run unit tests, focused Redis tests/races and review; record [evidence](evidence.md).
