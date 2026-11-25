# Intent: Sling 1.6.8 re-validation

Date: 2026-11-24. Source: `ea16062cf9d997b83137930acfa12842db814ac8`.
Requestor: user in this Claude Code session, for
[issue #656](https://github.com/redis/redis-adbc-driver/issues/656). Author:
Claude Code. Status: validated against a pre-release Sling build; the official
1.6.8 release is pending. [Specification](spec.md), [plan](plan.md),
[validation](validation.md).

The [Sling 1.6.6 record](../sling-postgres/intent.md) needed a template override,
a precreated table and direct truncate-and-load. Upstream closed
[Sling #836](https://github.com/slingdata-io/sling-cli/issues/836) with fixes for
1.6.8: ADBC `GetObjects` metadata without a template, zero-row batch handling and
catalog types on reads. Upstream tested DuckDB and SQLite, not Redis.

Re-run the PostgreSQL → Redis → PostgreSQL test with a stock generic ADBC
connection, add the cases the workarounds avoided, and retire or narrow the
guidance based on what passes. The user chose to test the upstream `v1.6.8`
branch before the release. Comment upstream only if something still fails.
Driver behavior, benchmarks, CDC and upserts remain outside scope.
