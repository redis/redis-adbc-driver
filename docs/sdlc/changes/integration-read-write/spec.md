# Specification

Date: 2026-11-01. Source: public `68b44d0`. Status: implemented.
[Intent](intent.md), [plan](plan.md), [verification](validation.md).

## Contracts

- The shared computed-output helper uses a uniquely owned table, preserves NULLs/Unicode and
  row order, compare complete fetched Arrow values/types through an independent
  connection, and clean up on success and ordinary failure. Empty output retains
  its schema. The example helper fully materializes results.
- Native paths keep their APIs: pandas/Polars DataFrame writes, SQLAlchemy bound
  CRUD, Airflow hook inserts, Dagster IO-manager assets, dlt append destination,
  Ray SQL writer, DuckDB ingestion, dbt builds and R literal SQL.
- dlt receives an explicit finite Arrow resource reading Redis; it does not gain
  CDC, incremental state or merge/replace support.
- Guide checks can require output-write/read-back evidence. A process exit of 0
  without that evidence fails. The multi-environment runner records both status
  and direction, clears stale summaries and supports a selected set of tools.
- Executable examples honor the configured Redis endpoint. Fivetran's source SDK
  and Superset retain their separate scope and are not counted among the 36.

## Boundaries and risks

Writes autocommit and may partially apply. No rollback, exactly-once retry or
multi-statement atomicity is supplied. Distributed readers remain readers; the
new finite output path runs on the caller unless a guide explicitly writes
inside a tool worker/task. Abrupt process termination can leave an example-owned
table. R positional-binding limits remain documented.

## Acceptance evidence

All 36 actual guides must pass with read/write evidence on Software with verified
4x QPF. Focused sink tests cover empty/nullable results, row order, quoted column
names and independent-reader failure cleanup. CI runs four lightweight guide
checks and the sink suite; heavier guide environments use the saved runner.
