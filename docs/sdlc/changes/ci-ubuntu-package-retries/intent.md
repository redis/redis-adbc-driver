# CI Ubuntu package reliability intent

Date: 2026-11-11. Requestor: alberttwong. Author: Codex.
Status: implemented for review. Base: `766dded08cc3728edfd092f5e4941e95932bfc9a`.
Request: fix failing [Actions](https://github.com/redis/redis-adbc-driver/actions).

The [DuckDB run](https://github.com/redis/redis-adbc-driver/actions/runs/43061068956)
failed installing ccache after Ubuntu DNS failures. Four Ubuntu distribution
jobs in the [release run](https://github.com/redis/redis-adbc-driver/actions/runs/43061068996)
failed before validation because apt accepted incomplete indexes, then could not
find prerequisites. Preserve every validation gate and improve setup resilience.
No driver behavior, Redis configuration, permissions or release publication changes.

See [spec](spec.md) and [plan](plan.md).
