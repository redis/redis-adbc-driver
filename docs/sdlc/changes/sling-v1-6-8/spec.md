# Specification

Date: 2026-11-24. Source: `ea16062cf9d997b83137930acfa12842db814ac8`.
Status: validated against a pre-release Sling build. [Intent](intent.md),
[plan](plan.md), [validation](validation.md).

Use Sling 1.6.8 with an explicit Redis shared library, verified TLS, Redis
Software with active/effective 4x QPF and noeviction, and disposable PostgreSQL.
Use no template override. Keep configuration and logs private and clean up only
test-owned data.

`scripts/test_sling_postgres.py` must run, through the stock connection:

- the empty-result reproducer `SELECT label FROM <schema>.inbound WHERE 1=0`;
- default full-refresh staging into a new and then an existing Redis table, with
  no precreation;
- two truncate/direct loads into a precreated Redis table;
- Redis → PostgreSQL exports of the staged and precreated tables, each requiring
  10,003 rows and zero differences in both `EXCEPT ALL` directions;
- empty sources: default full-refresh (no target or staging table left),
  `SLING_ALLOW_EMPTY=TRUE` staging (empty table created), truncate/direct (target
  emptied) and an empty Redis table exported with `SLING_ALLOW_EMPTY=TRUE`;
- `sling conns discover --columns` on the staged table;
- a final check that no `_tmp` staging table remains.

Column types on Redis and on the PostgreSQL copies are reported as observations,
not assertions; values are compared exactly. Every case runs even after another
fails. A case whose prerequisite failed is `not-run`, and a command absent from
the Sling build is `unavailable`. Any status other than passed exits nonzero.

The guide may drop the template and precreation requirements only for cases that
passed. Remaining DDL differences are documented with their observed types. Do
not claim official-release validation, `conns discover` support, atomic
replacement, schema equivalence or gated Arrow lane validation.
