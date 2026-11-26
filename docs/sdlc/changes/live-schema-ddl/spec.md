# Specification

Date: 2026-11-26. Source base: `49827869`. Author: Claude. See [intent](intent.md).

## Naming and opt-in

`adbc.redis.schema_ownership` (database or connection option, `managed` by
default, or `live`) selects the ownership of schemas created by SQL `CREATE
SCHEMA`. `live` requires `adbc.redis.live_tables=true` and the `redis` catalog.
Existing schemas keep their ownership: `CREATE SCHEMA IF NOT EXISTS` is a no-op
for any existing schema name, and without `IF NOT EXISTS` an existing name is
SQLSTATE 42P06. With `live_tables=true`, `CREATE SCHEMA IF NOT EXISTS redis_live`
succeeds as a no-op. `redis_live` itself cannot be dropped or renamed.

A live schema name is valid UTF-8 without NUL, at most 1,024 bytes, outside
`public`, `redis_live`, `redis_foreign`, `information_schema`, the `pg_` prefix
and temporary schema names. At most 1,024 live schemas exist; each keeps the existing
1,024-registration bound.

## Storage

The managed catalog key `adbc:{meta}:live_schemas` maps each live schema name to a
random 32-hex schema ID, and the name is also a member of the managed schema set.
Both change in one managed metadata publication, so managed and live schema names
share one uniqueness check, and every managed object creation reads the live map
(a publication dependency) before creating into a schema. Live schemas never
contain managed tables, views or sequences.

Live registrations stay in the separate control namespace: `redis_live` keeps
`adbc:{meta}:live:1:tables`; a named schema uses
`adbc:{meta}:live:1:schema:<id>:tables`. Named-schema records store the schema ID
and an empty schema name, so a schema rename never rewrites table records.
Existing `redis_live` records are unchanged; there is no storage migration.
`adbc:{meta}:live:1:schema:<id>:state` holds a `dropping` tombstone during
`DROP SCHEMA`.

## Statements

- `DROP TABLE [IF EXISTS] t[, …]` on live tables runs the existing unregister
  lifecycle for each, after preflighting all names: drop the driver-owned index
  without DD, then remove the registration. Source HASHes are never read,
  rewritten or deleted. Live and non-live targets cannot be mixed in one
  statement. An unfinished evolution is refused, as for the CLI.
- `ALTER TABLE [IF EXISTS] live.t RENAME TO u` atomically moves a `ready`
  registration to a new name in the same schema (42P07 if taken). Index, prefix,
  keys and LiveVersion are unchanged. Statements already using the old name fail
  their identity check. Every other `ALTER TABLE` action on a live table is 0A000.
- `DROP SCHEMA [IF EXISTS] s [CASCADE]`: a non-empty schema without CASCADE is
  2BP01. CASCADE preflights and unregisters every registration (HASHes kept).
  The empty schema then gets an atomic `dropping` tombstone (refused if a
  registration appeared), its catalog entry is removed by one publication, and
  the tombstone/registry are then deleted best-effort. An interrupted drop leaves
  the schema visible but refusing new registrations; rerun `DROP SCHEMA`.
- `ALTER SCHEMA s RENAME TO n` updates the name map, managed schema set and
  namespace OID in one publication. Table records and in-flight live statements
  keep the schema ID. Managed `ALTER SCHEMA` is 0A000.

- `CREATE TABLE [IF NOT EXISTS] t (col TYPE [NOINDEX] [COMMENT …], …) [WITH
  (prefix = '…')] [COMMENT …]` in `redis_live` or a live schema creates a
  registration with a fresh driver-owned index. The default prefix is
  `<schema>:<table>:`, with parts escaped like managed key parts (no extra `:`,
  `adbc` → `%61dbc`). The prefix must pass the live prefix rules, hold no keys,
  and overlap no Search index (every live registration's index, including a
  renamed table's, plus application indexes). Columns are nullable; `NOT NULL`,
  `DEFAULT`, identity/`SERIAL`, `CHECK` and key constraints are 0A000 because
  direct writes bypass them and the planner must not trust them. `prefix` is the
  only storage parameter; managed `CREATE TABLE` refuses `WITH (…)`. The Arrow
  creation path shares the same index policy and reserve/index/publish lifecycle,
  unchanged in behavior. As with Arrow creation, two concurrent creations of
  different tables on one new prefix are not fenced against each other.
- `CREATE TABLE t [WITH (…)] AS SELECT …` plans and evaluates the query in the
  isolated live executor (live tables only) before creating anything, derives
  columns as managed CTAS does, refuses all-NULL rows, creates the registration,
  then inserts generated-key rows through the live key-row path. A failure after
  creation leaves the table and acknowledged rows; the error says so.

Live schema DDL runs outside the managed statement workspace: a no-op managed
publication must never report a failure for a committed control change.

## Registration, writes and reads

Register, evolve, resume, cancel, unregister, Arrow ingestion, SQL DML/MERGE,
SELECT, metadata listing and `live_hash` partitions accept named live schemas.
Registration reserves a record only if no `dropping` tombstone exists, then
re-reads the catalog and withdraws the reservation if its schema ID is no longer
mapped. The only orphan case is a lost reply to that withdrawal; it leaves an
unreachable `building` record and no index.

`live_hash` partition descriptors carry the logical schema name and resolve it
under their retained producer catalog pin; like other in-flight live statements
they keep reading after `ALTER SCHEMA … RENAME`, and fail once the table is
unregistered.

Managed table lookup is unchanged and runs first. Only a managed miss, with
`live_tables=true`, consults the live name map. Live writes into named schemas
resolve the target under the managed write pin, then use the existing isolated
live executor. All existing live invariants hold: application-owned HASHes,
driver-owned indexes without DD, binary keys, full predicate rechecks, TTLs,
unknown fields and no replay of uncertain mutations.
