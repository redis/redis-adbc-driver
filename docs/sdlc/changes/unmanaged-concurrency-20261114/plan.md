# Plan

[Intent](intent.md), [specification](spec.md), [validation](validation.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

1. Correct hot digest budgeting and independently bound cold-body repair.
2. Check actual RESP encoded request sizes, cold repair, row order, oversized
   native fallback and a late unknown outer reply before any repair.
3. Use the coordinated Redis Software slot only after verified 4× QPF; collect
   retrieval-only one/four-client attribution before selecting server changes.
4. Verify actual full SQL one/four-client behavior for any selected improvement.
5. Retain all observations, slower cases and source identities; keep operational
   logs/identifiers private and release every client before yielding the slot.
