# Intent

Date: 2026-11-25. Source baseline: `252b01a4`.
Status: implemented on a branch; awaiting human review, not merged.
Requested by the user: "Q72 first", after [PR #686](https://github.com/redis/redis-adbc-driver/pull/686).
Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

After #686, TPC-DS Q72 still timed out at SF 0.1. It writes nine inner joins, fact first, then two LEFT JOINs. Any outer join made the whole join keep its written order. So `catalog_sales` was joined to `inventory` on `item_sk` alone first, about 18.7 million rows at SF 0.1, and the driver allocated 85 GB in 120 s.

Desired: inner joins written before trailing LEFT JOINs are ordered as an inner-only join would be, with results unchanged.

Excluded:
- RIGHT and FULL joins, LATERAL items and table functions.
- Inner joins written after an outer join.
- Q35, whose time is in EXISTS semi-join reads.
