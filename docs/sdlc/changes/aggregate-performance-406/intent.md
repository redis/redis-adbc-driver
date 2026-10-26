# Exact aggregate performance: #406

2026-10-21. Requested by Albert Wong: finish the remaining performance issues.
Baseline current main e692a70 after #425/#426. Status: implementation merged; local/CI verification, full-scale comparison,
profiles, preservation and window release complete after passing backup/restore. See [spec](spec.md), [plan](plan.md).

[#185](https://github.com/redis/redis-adbc-driver/issues/185) identifies Q2,
Q4, Q9 and Q10 client/transfer costs in a historical 100M-row ClickBench run.
Reduce supported-path work without relaxing exactness, NULL/default semantics,
BIGINT precision, ordering/ties, ownership, readiness, cancellation or memory
bounds. Preserve historical measurements; do not infer full-scale gains from
prefix profiles. #411 is closed; #410 merged as #426 and is now closed.
