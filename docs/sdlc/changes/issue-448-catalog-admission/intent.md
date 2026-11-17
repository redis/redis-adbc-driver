# Independent catalog writes: intent

Date: 2026-11-16 (America/Los_Angeles). Requestor: repository user.
Baseline: `225e0056`. Status: implemented and validated locally; pending maintainer review.
Issue: [#448](https://github.com/redis/redis-adbc-driver/issues/448).

Independent schema/table/view DDL conflicts on shared membership sets, physical
registries and the OID allocator. Four-thread dbt builds must finish without
callers replaying SQL. Coordinate catalog writers before execution while retaining
same-resource/dependency checks, leased ownership and uncertain-outcome handling.
No multi-statement transactions or SQL/Arrow replay are introduced.

See [specification](spec.md), [plan](plan.md) and [validation](validation.md).
