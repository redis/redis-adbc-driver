# Intent

2026-11-17. Requestor: Albert Wong. Author: Codex. Base: `3671fca7`.
Status: implementation and validation in progress.
See [specification](spec.md), [plan and evidence](plan.md).

Repair the failing [nightly Test workflow](https://github.com/redis/redis-adbc-driver/actions/runs/44055008215)
without weakening production admission, stale-publication protection, or Arrow
metadata assertions. Both Go architectures time out in old nested-write race
fixtures; Ray worker expectations omit the SQL declarations introduced by #454.
No production behavior, workflow permissions, benchmark, or support scope changes.
