# Implementation plan

Date: 2026-10-20. Source: `a1eec58`. Status: implemented and locally verified; CI/platform checks pending.
[Intent](intent.md), [spec](spec.md), [results](validation.md).

## Ordered work and files

1. Inspect requested issues/current implementation. #271 is already implemented;
   retain/extend end-to-end coverage. Upstream DuckDB #19/#20 are closed; verify
   actual behavior before retaining the issue's older failure assumptions.
2. Add `python/adbc_driver_redis` and TLS alias, optional integrations and tests.
   Keep the package here so versioned native and Python assets build together.
3. Add shared manifest code and standalone tarball installer, wheel assembly
   helper, release asset integration, and Linux CI integration tests.
4. Run focused and integration tests on dedicated Redis 8.6.2, a clean wheel
   environment, and separate Superset 6.1/SQLAlchemy 1.4 environment.
5. Update README and client guides, record evidence and limitations, review diff.

## Decisions

Use existing PostgreSQL catalogs rather than duplicating all reflection queries;
SQLAlchemy 1.4's int2vector expectation needs a small index-reflection override.
Read Redis vendor version through ADBC, but use PostgreSQL 16 as the catalog
compatibility version. Choose a local ConfigurableIOManager using public Dagster
interfaces rather than contributing upstream or importing private DbIOManager
internals. Explicit transactions stay rejected at the native boundary.

## Release and remaining work

Release CI attaches wheels alongside existing tarballs and tests Linux wheel
connections. macOS release CI checks native loading; this local session verifies
macOS connections on a dedicated Redis container. PyPI trusted publishing and
external registry listing remain deferred. No merge/release/reviewer approval is
recorded. Actual check outcomes belong in validation.md.
