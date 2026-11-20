# Live MERGE plan

Date: 2026-11-20. Base: `27e5a8d2`. Requestor: Albert Wong. Author: Codex.
Issue: [#553](https://github.com/redis/redis-adbc-driver/issues/553).

1. Extend live write dispatch to MERGE, preserving isolated execution.
2. Harden shared live UPDATE with a per-row non-replaying script path and identity/cancellation checks; preserve other table modes.
3. Exercise MERGE actions, RETURNING/default keys, exact binary keys, ownership, preflight errors, mixed-mode exclusions and injected failures on verified Redis Software/QPF4.
4. Update ownership/SQL documentation and record actual checks/review.

See [intent](intent.md), [spec](spec.md), [validation](validation.md).
