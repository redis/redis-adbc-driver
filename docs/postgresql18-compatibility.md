# PostgreSQL 18 SQL dialect compatibility through ADBC

Target selected on 2026-11-09; tracked in
[#118](https://github.com/redis/redis-adbc-driver/issues/118). This is a working
inventory at source revision `08d75d50888374b805a5c89f5bb94a46cf6fe8a9`, not a
claim of complete compatibility. It replaces the proposed SQL:2023 Core audit
as the objective for this change. Standards extensions #356–#358 remain separate.

## Existing resources and their limits

- PostgreSQL publishes [18's regression suites](https://www.postgresql.org/docs/18/regress.html),
  covering standard SQL and PostgreSQL extensions. Reuse relevant SQL inputs;
  record adaptations and omissions for psql, server internals, extensions, and
  protocol-specific tests. Running them through ADBC requires a suitable harness.
- PostgreSQL's [feature table](https://www.postgresql.org/docs/18/infoschema-sql-features.html)
  is a standards-support inventory. Its `is_verified_by` column is always null
  because PostgreSQL does not formally test feature conformance.
- [sqllogictest](https://www.sqlite.org/sqllogictest/doc/trunk/about.wiki)
  checks query answers; its documented scope excludes transactional behavior,
  concurrency, and locking. It can supplement a query corpus, not replace those
  checks or establish SQL:2023 conformance.
- The [existing ADBC differential harness](../go/pgdiff/README.md) compares
  results, physical Arrow types, and SQLSTATEs. It now selects PostgreSQL 18.6.
  Its curated PostgreSQL inputs and captured import oracle retain REL_16_15
  provenance; this is not an import of the PostgreSQL 18 regression suite.

## Initial source audit

“Present, unverified” means related implementation and tests exist; it does not
mean the complete area matches PostgreSQL 18. “Gap” describes a directly visible
implementation limitation. Existing tests listed here are candidate evidence,
not a statement that they have been rerun against PostgreSQL 18.

| Area | Initial assessment | Source and candidate evidence |
|---|---|---|
| SELECT, expressions, joins, subqueries | Present, unverified | [parser](../go/sql.go), [query execution](../go/query.go), [SQL tests](../go/sql_integration_test.go), [query forms](../go/query_forms_integration_test.go) |
| Aggregation and set operations | Present, unverified | [aggregates](../go/aggregate.go), [set operations](../go/setop.go), [aggregate tests](../go/aggregates_integration_test.go) |
| Numeric, string, date/time and NULL semantics | Present, unverified | [types](../go/types.go), [evaluator](../go/eval.go), [scalar tests](../go/scalar_test.go), [datetime precision tests](../go/datetime_precision_integration_test.go) |
| DDL, schemas, views and dependencies | Present with gaps; unverified overall | [parser](../go/sql.go), [views](../go/view.go), [drop behavior](../go/drop_objects.go); CREATE VIEW parsing does not implement WITH CHECK OPTION |
| INSERT/UPDATE/DELETE/MERGE and defaults | Present, unverified | [DML](../go/dml.go), [defaults](../go/defaults.go), [default tests](../go/defaults_integration_test.go), SQL integration tests |
| Integrity constraints | Gap | [primary keys](../go/pkey.go) retain metadata and imply NOT NULL but do not enforce uniqueness; [REFERENCES parsing](../go/sql.go) discards foreign-key definitions; [constraint tests](../go/constraints_integration_test.go) |
| Transactions and isolation | Gap | [connection](../go/connection.go) rejects disabling autocommit and explicit ADBC Commit/Rollback; [session control](../go/session.go) accepts SQL transaction controls without data rollback; statement snapshots do not establish multi-statement transaction compatibility |
| Privileges | Partial SQL surface | [privilege parser](../go/privileges.go) supports only table SELECT grants/revokes to provisioned roles; [tests](../go/privileges_test.go) reject grant options, column grants and other forms; [RBAC contract](sql-rbac.md) |
| SQL cursors and row locking | Gap requiring scope/design decisions | [statement dispatch](../go/sql.go) lacks DECLARE/OPEN/CLOSE cursor statements; Search cursors and Arrow streaming are not SQL cursor semantics; row-locking work is [#116](https://github.com/redis/redis-adbc-driver/issues/116) |
| Information schema and pg_catalog | Present subset, unverified | [information schema](../go/infoschema.go), [pg_catalog](../go/pgcatalog.go), [metadata tests](../go/infoschema_test.go); view names alone do not prove standard or PostgreSQL column/visibility semantics |
| Parameters, Arrow schemas, fetch and errors | Present, unverified against 18 | [parameters](../go/params.go), [Arrow conversion](../go/arrowconv.go), [SQLSTATE mapping](../go/sqlstate.go), [runner](../go/pgdiff/run.py) |
| Procedures, triggers, protocol and other exclusions | Explicitly scoped in #118 | Preserve the tracker's exclusions. An excluded feature must qualify any compatibility claim; it is not a passing case. |

## Completion evidence

1. Pin the PostgreSQL server image and upstream regression source separately.
   Capture actual server version, locale, encoding, time zone and session setup.
2. For each supported SQL form, map stable test IDs to its semantics: results,
   types, NULLs, ordering, errors, parameters, metadata and relevant concurrency.
3. Import relevant PostgreSQL 18 SQL with its license, hashes and adaptation
   records. Keep PostgreSQL 16 evidence under its actual version; do not rename
   the old corpus or assume legacy issue closures prove compatibility with 18.
4. Run the corpus and ADBC integration checks on Redis Software with verified
   active 4x QPF. Record skips, exclusions, infrastructure errors and mismatches
   separately. Keep credentials and operational artifacts private.
5. Resolve mismatches or document the supported subset and each exclusion.
   Publish reproducible evidence tied to the driver revision and configuration.

Acceptable claims must describe the evidence actually collected, for example
“passes the documented PostgreSQL 18 SQL comparison corpus through ADBC under
configuration X.” Passing a curated corpus, or even the upstream regression
suite, does not establish universal PostgreSQL compatibility or 177/177 SQL:2023
Core conformance. A separate [live curated comparison](sdlc/changes/postgresql18-target/validation.md)
measured 452/651 scored cases passing (69.4%), with 199 mismatches and no
infrastructure errors; that result does not validate the entire surface above.

The [#359 fix-and-classify change](sdlc/changes/postgresql18-359/validation.md)
subsequently measured 574/651 scored passes (88.2%), 77 failures, and no
infrastructure errors at implementation revision `b35a4739`. All 188 retained
baseline cases have dispositions: 122 observed passes and 66 deferred failures.
The 66 are exclusions from that implementation change, not from #118's
compatibility goal or from the full runner. [PR #366](https://github.com/redis/redis-adbc-driver/pull/366)
delivers the fixes; #118 and the owning feature issues retain the remaining work.
