# Intent

Date: 2026-11-26. Source base: `49827869`. Requestor: Albert Wong. Author: Claude.
Status: implementation in progress; no PR yet.

Pipeline integrations that default to live mode (Airflow, Dagster, Dask, Ray,
Prefect, dlt) issue ordinary schema DDL: `CREATE SCHEMA IF NOT EXISTS`, a staged
create, `DROP TABLE [IF EXISTS]`, `ALTER TABLE … RENAME TO`, and test cleanup via
`DROP SCHEMA … CASCADE`. Live registrations currently live only in the fixed
virtual `redis_live` schema, and every one of these statements is refused.
Unregistering is CLI/Go-only, so Python DBAPI callers cannot remove a live table.

The requestor chose (2026-11-26):

- **User-named live schemas**: SQL `CREATE SCHEMA`, `DROP SCHEMA` and
  `ALTER SCHEMA … RENAME TO` for schemas that hold live registrations, beside
  the built-in `redis_live`.
- **`DROP TABLE` on a live table unregisters only**: the driver-owned index and
  registration are removed; every application HASH, TTL and unknown field stays.

Live table `ALTER TABLE … RENAME TO` renames the SQL registration only. Source
keys, prefixes and indexes stay unchanged.

The requestor then asked (2026-11-26) for SQL `CREATE TABLE` in live schemas,
including `CREATE TABLE … AS SELECT`, so SQL tools can create live tables without
Arrow `live_source` configuration or the CLI.

Out of scope: deleting source HASHes from DDL; constraints and defaults on live
columns; reading managed tables in a live `CREATE TABLE … AS SELECT`; managed
schema rename; column-level live DDL (still `live evolve`); moving tables between
schemas; Flex. See [spec](spec.md) and [plan](plan.md).
