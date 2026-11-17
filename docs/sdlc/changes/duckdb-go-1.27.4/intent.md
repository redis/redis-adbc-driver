# Intent: repair the DuckDB toolchain bootstrap

Date: 2026-11-17. Requestor: repository user. Author: Codex.
Source: `2b6634e0`. Status: implementation and validation in progress.

The user requested a fix for failing repository Actions. The
[DuckDB run](https://github.com/redis/redis-adbc-driver/actions/runs/44168466706)
failed because the module now requires Go 1.27.4 while the extension bootstrap
still downloads Go 1.27.3 with `GOTOOLCHAIN=local`.

Restore the extension build on supported Linux/macOS architectures without
changing driver behavior, supported servers or historical test evidence.
See [specification](spec.md) and [plan/evidence](plan.md).
