# Specification

Date: 2026-10-20. Source: `a1eec58`. Status: implementation.
[Intent](intent.md), [plan](plan.md), [verification](validation.md).

## Contracts

- #271: existing C/POSIX/default byte collation, unsupported names return 42704,
  explicit column collation appears in information_schema; keep regression checks.
- #276: `adbc_driver_redis.connect` returns an ADBC database; `dbapi.connect` returns
  an autocommit connection. False is rejected. Bundle the library in release
  wheels tagged py3-none for glibc 2.28 amd64/arm64 and macOS 12 arm64 (Go 1.26 minimum; inspect Mach-O before tagging). PyArrow is
  optional until DB-API use. `adbc_driver_rediss` supports URI module lookup.
- #277: explicit installers write redis/rediss manifests with absolute paths.
  Support user, venv, system and custom directories, without implicit elevation.
  Native tarball installation needs only Python, not the wheel or manager.
- #273: redis+adbc/rediss+adbc SQLAlchemy URLs, qmark binding, PostgreSQL 16
  compilation/catalog reflection, default schema from ADBC and Redis vendor
  version from GetInfo. Support SQLAlchemy 1.4/2.x. Superset entry points register
  PostgreSQL parsing/time grains. AUTOCOMMIT only; housekeeping begin/commit/
  rollback cannot undo data. Reject savepoints and two-phase transactions.
- #275: local IO manager accepts Arrow tables/readers and pandas/polars. Stage
  unpartitioned outputs before DROP/RENAME. Delete and append static/string or
  time-window partitions using bound values. Column projections and output
  metadata are supported. Unique stages prevent staging name collisions; callers
  must serialize writes to the same asset. Reads fully materialize before return.
- #278/#280: check current adbc_scanner reads, joins, functions, autocommit INSERT
  and CTAS, read-only refusal, explicit-transaction rejection and unchanged rows.
  Inspect scan filter/projection plan. Document older builds and untested Columnar.

## Boundaries and risks

Native driver storage/lifecycle semantics remain unchanged. Staging ingest failure
preserves the previous version; DROP/RENAME remains non-atomic and a failure after
DROP can lose that version. Partition append failure can leave partial data.
Manifests overwrite the two named files only when installation is requested.
Native extension downloads are gated by an explicit test environment variable.

## Acceptance evidence

Tests must fetch complete results and compare values. Cover a clean installed
wheel without REDIS_ADBC_LIBRARY, direct and URI manifest discovery, actual Dagster
materializations/retries/failure, both SQLAlchemy generations, and Superset's own
metadata/query paths. CI covers release architectures; do not equate configured
jobs with successful runs. No performance claim follows from a three-row plan.
