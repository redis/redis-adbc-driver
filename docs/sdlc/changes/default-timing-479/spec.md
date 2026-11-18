# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#479](https://github.com/redis/redis-adbc-driver/issues/479).

Validate forbidden references, subqueries, parameters, aggregates/window calls, signatures, expression types and assignment compatibility without executing default expressions. Unknown literals still take the column or explicit-cast type and reject invalid type input at definition time; typmod value checks remain deferred. Preserve local-time literal freezing and SQL metadata. Evaluate defaults when used by SQL/Arrow writes, with immutable planning checks on referenced SQL defaults even for empty sources. ADD COLUMN continues materializing nonvolatile defaults and enforcing existing volatile-ADD restrictions. Preserve NULLs, lazy branches, statement/session/volatile semantics, sequence non-replay and atomic publication.

Retained unknown-literal typing across ALTER TYPE remains a separate pre-existing defect ([#484](https://github.com/redis/redis-adbc-driver/issues/484)); the default grammar issue is [#481](https://github.com/redis/redis-adbc-driver/issues/481). No new transaction or volatile ADD COLUMN support is claimed.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
