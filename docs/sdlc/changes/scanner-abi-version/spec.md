# Scanner ABI version: specification

Date: 2026-10-21. Source: 7025103. Status: implemented locally.
See [intent](intent.md) and [plan/evidence](plan.md).

The composite action accepts a bare stable `2.0.x` wheel version, checks out
the corresponding `v2.0.x` tag, and stamps that same prefixed ABI version.
DuckDB's CMake reads the `DUCKDB_VERSION` environment variable before
`OVERRIDE_GIT_DESCRIBE`; the action must keep its bare input in a separate
`SCANNER_DUCKDB_VERSION` variable so the existing override takes effect.

No runtime driver behavior, dependencies, permissions, or test skips change.
Acceptance: reproduce the unprefixed stamp with the old environment, obtain the
prefixed stamp with the corrected environment, and pass workflow syntax checks.
Hosted Python loading remains an integration check for the next Actions run.
