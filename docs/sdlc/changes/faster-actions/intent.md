# Faster GitHub Actions

Date: 2026-10-20. Source: `c74ebeb`. Status: implemented locally; CI pending.
Requestor: Albert Wong, following investigation of long Actions runs.
Issue/PR: see the pull request containing these records.

Reduce repeated DuckDB compilation and unnecessary PR workflow runs. Observed
native C++ builds took about 12 minutes on x86 and macOS; ARM was still building
after 17 minutes. Release PR checks also run for Python README changes.

Keep all architecture, Redis environment, SQLLogicTest and release validation
coverage. Runtime improvements require subsequent hosted-run measurements.
See [spec](spec.md) and [plan/evidence](plan.md).
