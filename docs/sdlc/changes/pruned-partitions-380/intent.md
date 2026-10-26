# Intent

2026-10-21; issue [#380](https://github.com/alberttwong/redis-adbc-driver/issues/380).
Baseline PR #385; requestor alberttwong; author Codex.
Status: implemented and locally validated; no maintainer approval.

Avoid unrelated partition work for selective internal row-ID predicates, and
preserve bounded direct HASH reads through the partition API. Keep supported
SQL/layouts, typed schemas, cap safety and live-read consistency unchanged.
See [spec](spec.md), [plan/evidence](plan.md).
