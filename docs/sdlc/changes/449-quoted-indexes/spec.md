# Specification

Date: 2026-11-16 (America/Los_Angeles). Source: `225e0056`.
Author: Codex. Requestor: Albert Wong. Status: implemented locally; review pending.
Issue: [#449](https://github.com/redis/redis-adbc-driver/issues/449).
See [intent](intent.md) and [plan and evidence](plan.md).

`CREATE INDEX` resolves each exact existing SQL column and preserves existing
type/capability checks. For unsafe physical field names it allocates a private
Search alias without interpolating the SQL identifier into Search expressions.
Aliases cannot collide with another alias, physical field or retired field.
Column metadata persists the alias independently of the SQL name and HASH field.
Every index build/alter emits the original HASH identifier and its separate `AS`
alias as command arguments. Safe existing fields keep their original bindings.

Named index membership remains expressed in physical HASH fields; PostgreSQL
catalog metadata maps those back to logical names. Materialized-view refresh,
column/table rename and private generation rebuild retain the alias. DROP INDEX
removes only SQL membership; column/schema removal uses existing cleanup rules.
The optional metadata field is absent for preexisting unaliased columns; no
storage protocol migration is required. Existing query safety guards and exact
HASH reads remain in place, including NULL handling; aliases do not enable new
expression pushdown.

Acceptance covers numeric and TAG native alias predicates; spaces, mixed case,
embedded quotes/punctuation; SQL rows and NULLs; index metadata, refresh, rename,
column/schema cleanup, sorting, DISTINCT, aggregates and SQL index-name reuse. Unit checks cover collisions,
metadata persistence and SQLSTATE `0A000` for UNIQUE, expression and partial
forms. Runtime evidence requires Redis Software with verified effective 4x QPF.
