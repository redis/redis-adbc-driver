# Intent

Date: 2026-10-21. Author: Codex. Requestor: user in this chat.
Base: 85d6807. Status: implemented in PR #382; local verification complete.

Fix failing DuckDB CI while selecting the newest stable 2.0.x patch release.
The Python integration's community `adbc_scanner` download returns 404, and
Linux native SQLLogicTests skip `require redis_adbc` after a successful build.
Preserve the existing integration assertions and reject missing/skipped tests.
See [spec](spec.md) and [plan](plan.md).
