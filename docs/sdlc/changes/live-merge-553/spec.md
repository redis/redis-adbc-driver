# Live MERGE specification

Date: 2026-11-20. Base: `27e5a8d2`. Requestor: Albert Wong. Author: Codex.
Issue: [#553](https://github.com/redis/redis-adbc-driver/issues/553).

Route live MERGE through the isolated live executor already used by INSERT/UPDATE/DELETE. Reuse common MERGE binding, join, clauses, defaults, type/constraint checks, duplicate-target validation and RETURNING. Validate all planned expressions before mutation; apply updates, deletes, inserts in that order. Successful counts include each action. Failures can leave earlier mutations applied, without a reliable complete count or RETURNING result.

Live UPDATE now uses one non-replaying EVAL per source row, retaining the existing atomic vanished-row/last-field guard, TTL and unknown-field preservation. Check registration identity, row prefix and cancellation before each UPDATE; DELETE also checks cancellation per row. Preserve original binary keys. Lost and composed replies stop without replay. INSERT retains bounded source pipelines and collision checks. Application edits can interleave; registration checks are not a fence against application writers. Mixed managed/live writes, modifying CTEs and retained preparation remain rejected.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
