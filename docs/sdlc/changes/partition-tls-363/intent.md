# Intent

2026-10-21; baseline 9f5188c; requestor alberttwong; author Codex.
Issue [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363).
Status: implemented and locally validated; no maintainer approval.

Require certificate-verified TLS partition reads from independent processes
using a restricted worker principal distinct from the planner. Exercise the
native library in PR tests and the installed wheel in release validation,
without provisioning paid resources or implying Cloud compatibility.
See [spec](spec.md), [plan](plan.md).
