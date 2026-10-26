# Intent

2026-10-21; issue [#177](https://github.com/redis/redis-adbc-driver/issues/177).
Baseline PR #385; requestor alberttwong; author Codex.
Status: implemented and locally validated; no maintainer approval.

Avoid unrelated partition work for selective internal row-ID predicates, and
preserve bounded direct HASH reads through the partition API. Keep supported
SQL/layouts, typed schemas, cap safety and live-read consistency unchanged.
See [spec](spec.md), [plan/evidence](plan.md).
