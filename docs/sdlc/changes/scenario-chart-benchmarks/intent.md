# Intent: numeric performance charts

Date: November 13, 2026 (America/Los_Angeles). Requested by the repository user; authored by Codex. Status: implementation and measurement; human review pending. Driver source ca685806. Review: [PR #436](https://github.com/redis/redis-adbc-driver/pull/436).

Replace qualitative chart gaps with reproducible point-read/INSERT/UPDATE/DELETE and filter/aggregate comparisons for managed, unmanaged and redis-py. Generate chart-ready JSON/CSV with units, counts, latency/rate and baseline multiples. Slide 4 needs numbers; slide 19 needs point timings; slide 20 needs numeric versus-client values. Remove slides 26–31 while retaining evidence links. No new 100M ClickBench run.

See [spec](spec.md) and [plan](plan.md).
