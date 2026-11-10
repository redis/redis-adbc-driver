# PostgreSQL 18 recursive LIMIT: spec

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `ce62690a`. Status: targeted implementation validated.
Issue: [#361](https://github.com/redis/redis-adbc-driver/issues/361).

Admit only a sole owned recursive CTE, a direct unfiltered projection and resolved finite LIMIT/OFFSET; both terms must produce at most one row per iteration. Do not bound sorts, joins, aggregate/window/distinct consumers, subqueries, shared references, SEARCH/CYCLE or multi-row recursive frontiers. Bounded results never enter the shared CTE cache. Broader recursive demand support remains outside this batch.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).


The row bound is resolved LIMIT plus OFFSET, guarded by subtraction against the
existing row budget to avoid integer overflow. The iteration checks cancellation
and stops before evaluating the next recursive row after satisfying demand.
UNION deduplication and early natural termination are unchanged. Planning-only
queries retain their existing schema-only behavior. Eligibility deliberately
rejects multi-row seed/frontier shapes because the current executor eagerly
computes each iteration. Native retained partition execution continues to reject
recursive CTEs pending separate admission.

[PostgreSQL 18 WITH documentation](https://www.postgresql.org/docs/18/queries-with.html)
describes demand-limited recursive evaluation and consumers that require full
results; this batch implements a restricted subset of that behavior.
