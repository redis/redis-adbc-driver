# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#479](https://github.com/redis/redis-adbc-driver/issues/479).

Fix #479 by separating DEFAULT definition validation from value evaluation. CREATE TABLE, SET DEFAULT and retained default checks during ALTER TYPE must not execute deferred values or consume volatile/sequence results. Preserve atomic INSERT/ALTER and definition-time type/literal errors.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
