# Implementation plan

Date: 2026-11-01. Status: implemented; local checks passed; platform CI pending.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Fetch/pull current organization main and inspect all 50 inventory rows, setup
   guides, existing installer lifecycle, dbc packaging and integration helpers.
2. Add a complete installation matrix and pandas/Polars convenience extras.
3. Reuse the existing DuckDB build/test workflow in release dependencies; package
   its checked binaries and automatically attach the existing managed installer.
4. Chain dedicated PyPI publishing to a completed release upload, retain manual
   retry/description revisions, and extract/test the wheel publication gate.
5. Validate inventory coverage, wheel/install helpers, workflow syntax, current
   native wheel build/load and manifests. Record current-source evidence, then
   commit and create a PR against `redis/redis-adbc-driver` main.
