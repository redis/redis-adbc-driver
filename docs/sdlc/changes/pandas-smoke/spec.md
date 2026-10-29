# Specification

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: locally verified.

Use pandas' native ADBC path through `adbc_driver_redis.dbapi.connect`.
Verify fetched values and dtypes for nullable integers, doubles, strings,
booleans and microsecond timestamps, including Unicode and an integer above
2^53. Verify append, filtered aggregation, default existing-table rejection
without changed rows, and replacement. Each test owns a unique table and
cleans it up in a fixture finalizer. Writes remain autocommit.

No compatibility claim for every pandas version or dtype follows from these
checks. See [intent](intent.md), [plan](plan.md), [validation](validation.md).
