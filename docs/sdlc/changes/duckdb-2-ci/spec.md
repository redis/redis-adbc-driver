# Specification

Date: 2026-10-21. Status: implementation contract; review pending.
See [intent](intent.md) and [plan](plan.md).

- Select `duckdb==2.0.*` in Python analytics CI and test extras. Resolve the
  installed patch version before building the C++ scanner for its exact tag.
- Build `Query-farm/adbc_scanner` v2.0 commit
  b7cd0afb4884ede6533f1534216bcb0de61bb76b, using its pinned dependency toolchain.
  Load the resulting local extension and run all existing integration assertions.
- Keep signed repository installation available for manually enabled tests;
  only explicitly supplied local builds enable unsigned extension loading.
- Repair native Linux test loading and surface actual loader errors instead
  of reporting only a missing requirement. Keep the zero-skips CI assertion.
- Validate the actual Linux loader, offline SQL tests, and DuckDB 2.0 integration.
