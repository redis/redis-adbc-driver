# Intent

Date: 2026-11-18. Requestor: Albert Wong. Source: 06302c39bd4cb5f8a4e0a1bfc978efc2d5d072ff. Status: implementation.

Reduce core PR wait time immediately by scheduling existing tests concurrently on isolated fixtures. The last green Test run took about 18 minutes; snapshots and ordinary Go tests dominated. Preserve all current correctness, race, platform and native validation coverage.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
