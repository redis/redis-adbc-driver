# Intent

Date: 2026-11-03. Requestor: Albert Wong. Author: Codex.
Base: `bb83527` from redis/redis-adbc-driver main. Status: implementation in review.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Implement the acceptance examples in issues
[#101](https://github.com/redis/redis-adbc-driver/issues/101),
[#102](https://github.com/redis/redis-adbc-driver/issues/102),
[#103](https://github.com/redis/redis-adbc-driver/issues/103),
[#104](https://github.com/redis/redis-adbc-driver/issues/104), and
[#105](https://github.com/redis/redis-adbc-driver/issues/105): generated keys,
UUIDs, modifying WITH, named index DDL, and materialized query results.
Consumers include application schemas, migration tools and analytics loaders.

Preserve HASH ownership, existing table/index generations, Arrow lifetime and
exact values. Do not introduce transactions, unique-index enforcement or
cross-connection consistency storage. Record implementation limits separately
from historical PostgreSQL and dbt evidence; full dbt acceptance remains unverified.
