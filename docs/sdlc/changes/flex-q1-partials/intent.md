# Intent

Requested 2026-11-25 by the user ("fix #663"), relayed by the coordinating session. Base: main `b1efe706` (includes #646). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

After #646, SF1 Flex Q1 ran the exact partitioned aggregate on 32 workers. It still took 238.9 s cold and 79.2 s warm, and allocated about 10.5 KB per row on the 4-CPU / 8 GiB client.

The coordinating session's order of work:
1. Profile, then cut what the Flex `partitioned_exact` read fetches and allocates. Keep exact decimal partials, NULL handling and the full residual predicate.
2. Measure whether a Flex grant above 32 helps cold reads, with client memory bounded. Change the default only if measurements support it.
3. Enable server-side aggregation only if it is exact and visibility-safe; otherwise say so.

The deployment is not to change. If Q1 cannot reliably finish under 60 s, record the measured bound.
