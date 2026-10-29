# Specification

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: locally validated.

`pandas.read_sql` and `read_sql_query` must accept the package's open ADBC
DB-API connection. Verify complete DataFrames against independently constructed
Arrow values for default and Arrow backends: large integers, nullable integer,
text, boolean, floating point and microsecond timestamp columns. Verify SQL
filtering, aggregation, index selection, typed empty results, caller connection
reuse and complete consumption of a source proven to contain multiple batches.

Tests use unique tables and drop them in fixture cleanup on a disposable Redis
Software database. No driver changes or new package dependencies are required.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
