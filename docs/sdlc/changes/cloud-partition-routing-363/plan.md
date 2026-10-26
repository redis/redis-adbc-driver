# Plan

Date: 2026-10-21. Source: merged #407. Author: Codex; requestor: Albert Wong (keep going; Redis Cloud Pro by default). Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).

1. Estimate then create one dedicated two-shard RAM Pro database.
2. Add fail-closed topology requirements and evidence to the Cloud runner.
3. Run shared checks on macOS and an installed Linux wheel; investigate OSS Cluster API separately if supported.
4. Verify ACL, database and subscription cleanup; publish exact scope and remaining gaps.

See [intent](intent.md) and [spec](spec.md). No runtime changes are planned.

Completed: live two-shard proxy validation, Linux installed-wheel automatic Cluster API validation through one advertised proxy owner, RESP3 default regression assertions, all ACL/resource cleanup, final OSS/unit checks and evidence. No runtime changes. See [evidence](evidence.md). Human review and hosted checks remain pending.
