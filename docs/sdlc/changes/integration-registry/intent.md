# Shared integration inventory

Date: 2026-11-09. Requestor: repository owner. Author: Codex.
Base: `cc3dad27c14901bacaef5c4a6b3cca55f43537a9`.
Issue: [#343](https://github.com/redis/redis-adbc-driver/issues/343).
Status: implementation complete; validation/delivery recorded on the PR.

Reduce repeated tool/profile inventories across the guide and native candidate
runners. Preserve public commands, dependency environments, exceptional runtime
hooks and fixture ownership. This is harness maintenance, not upstream integration
acceptance or new driver/SQL/snapshot behavior.

See [spec](spec.md), [plan](plan.md), [validation](validation.md).
