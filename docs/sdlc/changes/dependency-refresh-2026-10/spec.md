# Latest library requirements with isolated Superset compatibility

See [intent](intent.md) and [plan and evidence](plan.md).

- Resolve stable Go module releases and upstream revisions, including direct
  major upgrades. Migrate regexp2 to v2.9.1 and adapt renamed capture fields
  `Index`/`Length` to `RuneIndex`/`RuneLength`, preserving rune-to-byte conversion,
  leftmost-longest matching and the existing deadline.
- Keep Arrow Go v18.8.0, ADBC v1.12.0 and go-redis v9.23.0, already current.
  Update the remaining Go graph with Go tooling and regenerate checksums.
- Set current main Python floors: ADBC manager 1.12.0, PyArrow 26.0.0,
  SQLAlchemy 2.1.6, Dagster 1.13.26, Dask 2026.8.0, pandas 3.1.0,
  Polars 2.0.0, pytest 9.1.1, setuptools 84.2.1 and wheel 0.48.0.
  DuckDB tests use release 2.0.0. Harness floors include psycopg 3.3.6,
  sqlparse 0.6.0 and redis-py 8.1.0.
- Test current ADBC manager 1.12.0 in client matrices and explicit client checks;
  remove the old 1.8.0 compatibility checks rather than forcing a version below
  the new declared requirement.
- Isolate the `superset` extra: Superset >=6.1.0, SQLAlchemy >=1.4.54,<2,
  PyArrow >=18.1.0,<19, Flask-Caching ==2.3.1, cachetools >=7.2.4. Use Python
  3.12 for this CI environment. Do not combine it with the main extras requiring
  newer SQLAlchemy, Arrow or pandas. Older Superset transitive dependencies
  still have published advisories; do not suppress or claim remediation of them.
- Keep package metadata at Python >=3.12 to allow the approved Superset extra.
  Main CI/release/build environments use Python 3.14; harnesses require >=3.14.
  Do not select Python 3.15: Dagster excludes it and ADBC lacks binary wheels.

Acceptance: Go tests/builds and module verification pass using Go 1.26.10;
Go source vulnerability scan has no findings; current Python extras install and
unit tests pass with explicit skips. A native wheel builds/loads; isolated
Superset starts and registers both Redis engine specs. Report live Redis and
cross-platform gaps. Current release metadata checks are not proof that all
future allowed dependency combinations will be free of vulnerabilities.
