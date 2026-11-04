# Intent: SQL SELECT grants backed by Redis Software RBAC

Date: 2026-11-04. Requestor: Albert Wong. Author: Codex.
Source: `f1e1f03` (redis/redis-adbc-driver main). Status: implemented and locally verified; pending PR checks.
Issue: [#108](https://github.com/redis/redis-adbc-driver/issues/108).

Allow SQL grant scripts and dbt's single-role SELECT grant flow to change real
Redis read permissions. During implementation the requestor selected enforced
RBAC mapping, starting with table SELECT, and confirmed the dedicated ACL
approach. Compatibility no-ops are excluded.

Preserve administrator ACLs and role membership. Make incomplete mappings and
uncertain management writes fail explicitly. See [spec](spec.md) and
[plan/evidence](plan.md). No approval is inferred from generated records.
