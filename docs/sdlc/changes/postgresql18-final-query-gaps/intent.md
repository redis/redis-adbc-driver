# PostgreSQL 18 final query gaps: intent

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `b14aea15`. Status: targeted corpus cases validated. Issue: [#361](https://github.com/redis/redis-adbc-driver/issues/361).

Resolve the three remaining #361 corpus cases: planner constant errors in row-dependent CASE, numeric EXTRACT distinct from DATE_PART, and MD5 logical text through CTAS. Preserve all 599 prior passes and unchanged corpus/allowlists.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
