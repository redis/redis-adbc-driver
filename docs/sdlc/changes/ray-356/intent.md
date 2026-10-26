# Ray integration: validate the Arrow-table bridge

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: f5e90d8 (current main when validation started).
Status: bridge validation complete; worker-side datasource work remains pending.

The user requested work on [#164](https://github.com/redis/redis-adbc-driver/issues/164),
then specifically requested validation of the existing Redis ADBC → Arrow table
→ `ray.data.from_arrow()` path before extending the integration.

Prove that exact composition with a real Redis query, preserving rows, NULLs
and Arrow types after ADBC handles close. This phase does not implement a Ray
datasource or establish upstream acceptance.

See [spec](spec.md), [plan](plan.md), and [verification](validation.md).
