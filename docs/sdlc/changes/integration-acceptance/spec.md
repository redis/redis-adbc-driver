# Integration acceptance specification

Date: 2026-11-01. Source: `69482fb`. Status: implemented.
Related: [intent](intent.md), [plan](plan.md).

- Audit all 50 comparison tools, including the 38 guided integrations; link the
  inspected upstream sources and distinguish reusable suites, project-specific
  workflows, missing connectors, and dedicated external projects.
- Add independent SQLAlchemy and R DBI compliance processes. Reuse the real
  Fivetran SDK tester and existing ADBC validation. Exclude dbt and Airbyte.
- Run suites sequentially with isolated Python dependencies against dedicated
  authenticated TLS Software, verified noeviction and active 4x QPF. Never
  replace the current native library with a released connector's pinned wheel.
- Preserve upstream assertions and expose existing compatibility failures.
  Count passes/failures/errors/skips/xfails; fail on missing/invalid reports,
  no passing cases, xfails, setup errors and nonzero process exits.
- Remove stale per-suite evidence, continue after independent suite failures,
  sanitize credential-bearing output, and retain JUnit/JSON/log artifacts.
- Provide Make/candidate entry points and a manual Actions workflow; PR helper
  checks validate reporting and the 50-row inventory. Full new contracts are not
  claimed to be passing required PR checks.

Risks: upstream compliance suites create/drop database objects and may expose
unsupported SQL or DBI features. Use an empty disposable database. SQLAlchemy's
default requirements do not declare full Redis capability support. R package
and some Python dependency ranges remain floating; package versions appear in
installation logs. No adapter implementation or driver SQL fix is in scope.
