# Intent

Date: 2026-11-16 (America/Los_Angeles). Source: `225e0056`.
Author: Codex. Requestor: Albert Wong. Status: implemented locally; review pending.
Issue: [#449](https://github.com/redis/redis-adbc-driver/issues/449).
See [spec](spec.md) and [plan and evidence](plan.md).

dbt exact-column named-index configurations fail for valid quoted SQL columns
such as `"Customer ID"`, even though table creation and reads work. Support
these indexes on managed ordinary tables and materialized views through safe
Search bindings, preserving logical names and HASH ownership.

Exclude expression, partial and UNIQUE indexes, new indexable types, Flex DDL,
application-owned index changes, new query pushdown and the independent catalog
concurrency issue #448. No dbt adapter capability claim or release is implied.
