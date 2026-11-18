# Intent

Date:2026-11-17. Requestor:Albert Wong. Author:Codex.
Base:`5b0840ce`. Status:implemented; local verification complete; PR review pending.
Issue:[#110](https://github.com/redis/redis-adbc-driver/issues/110).

Add session-local SETSEED and RANDOM_NORMAL under #110 and make existing RANDOM share that stream, so seed/reseed and interleaved SQL draws have reproducible PostgreSQL18 semantics without changing snapshot replay rules.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
