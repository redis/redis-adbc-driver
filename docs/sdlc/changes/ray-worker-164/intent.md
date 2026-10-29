# Ray worker reader intent

Date: 2026-10-29. Requestor: repository maintainer in the implementation chat.
Author: Codex. Source baseline: `34ae626`. Status: implemented; human review pending.
Tracker: [#164](https://github.com/redis/redis-adbc-driver/issues/164).
See [spec](spec.md), [plan](plan.md) and [validation](validation.md).

Enable Ray Data preprocessing without materializing a Redis query on the caller
or converting Arrow data through Python rows. Ship a Redis-specific optional
reader in this repository rather than waiting for upstream acceptance.

Success means eager public ADBC partition planning followed by incremental
worker-local Arrow reads, complete static-data coverage, schema fidelity,
resource cleanup and clear errors/consistency limits. Exclude writes, Flight SQL,
manual query splits, retained results and generic multi-driver abstractions.
No driver-level consistency or transaction feature is introduced.
