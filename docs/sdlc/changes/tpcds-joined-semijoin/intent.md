# Intent

Date: 2026-11-25. Source baseline: `252b01a4`.
Status: implemented on a branch; awaiting human review, not merged.
Requested by the user ("Q72 first", then Q35).
Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

TPC-DS Q35 timed out at SF 0.1, and Q69 took 29 s. Their correlated EXISTS bodies join a fact table to `date_dim`, and `planSemiJoin` refused any body containing a join. So each body ran once per outer customer row: Q35 ran it 2,492 times in 120 s and built no semi-join.

Desired: a correlated EXISTS or IN whose body joins tables is decorrelated like a single-table body, with the same results. It must not get slower when only a few outer rows reach it.

Excluded:
- Page probes for joined bodies.
- Bodies with views, lazy relations, or outer references outside the correlation conjuncts (for example in JOIN … ON).
- SF1 runs.
