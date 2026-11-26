# Plan

Date: 2026-11-26. Source base: `49827869`. Author: Claude.

1. Add the schema reference model: catalog name map, per-schema control registry,
   stored schema ID with record normalization, and the ownership option.
2. Route live SQL DDL (`CREATE/DROP/ALTER SCHEMA`, `DROP TABLE`, `ALTER TABLE …
   RENAME TO`) before the managed statement workspace; guard managed creation and
   managed `DROP SCHEMA` against live names.
3. Thread schema references through registration, evolution, resume, cancel,
   unregister, Arrow ingestion and replacement, SQL DML/MERGE, SELECT, catalog
   listing and `live_hash` partitions. Add CLI `-schema` and Go `…InSchema` APIs.
4. Regenerate the Redis Functions inventory.
5. Tests: unit parsing/validation; integration on verified Software QPF4 for
   create/drop/rename schema and table, IF [NOT] EXISTS, CASCADE, HASH/TTL
   preservation, name collisions with managed schemas, registration into a
   dropping schema, in-flight identity after table rename, Arrow ingest and DML
   in named schemas, and existing live regressions.
6. Update live/driver reference/feature docs; record validation evidence.
7. Follow-up request: SQL `CREATE TABLE` and `CREATE TABLE … AS SELECT` for live
   tables, `WITH (prefix = …)` parsing, prefix/index-overlap refusal, constraint
   refusals, shared Arrow creation helpers, and their unit/integration tests.

See [intent](intent.md) and [spec](spec.md).
