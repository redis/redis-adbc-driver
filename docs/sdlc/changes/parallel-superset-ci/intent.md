# Intent: parallel Superset CI

Date: 2026-11-04. Source: `85a9bf3`. Status: implemented; CI execution pending.
Requestor: Albert Wong. Author: Codex.

Reduce PR feedback time by removing Superset installation and tests from the
amd64 driver integration job. Current upstream already runs shared helpers once.
Preserve existing Superset coverage, dependencies and authenticated TLS Redis
Software with verified 4x QPF. No driver or release behavior changes.

See [specification](spec.md) and [plan and validation](plan.md).
