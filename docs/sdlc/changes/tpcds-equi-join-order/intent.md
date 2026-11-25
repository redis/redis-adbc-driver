# Intent

Date: 2026-11-25. Source baseline: `064eb12d`.
Status: implemented on a branch; awaiting human review, not merged.
Requested by the user through the TPC-DS follow-up issues.
Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

TPC-DS Q11–Q99 had never run against the driver. A small-scale pass on current `main` (SF 0.1, Redis Software with verified 4× QPF) found five queries over the 60-second deadline. Three of them, Q15, Q19 and Q45, read only a few MB from Redis but allocated 45–104 GB in the driver. Their inner-join order paired a table with every joined row because of a condition that is no join key: an OR across tables in Q15 and Q45, and `<>` in Q19. Q15 finished correctly, but only after 332 s.

Desired: when choosing the next table of an inner join, prefer one that an equality joins as a hash key. Results must not change.

Excluded:
- Q35, which is bound by Redis I/O in `FT.AGGREGATE` during a snowflake join.
- Q72, whose LEFT JOINs keep the written join order.
- Outer and lateral join ordering, and SF1 runs.
