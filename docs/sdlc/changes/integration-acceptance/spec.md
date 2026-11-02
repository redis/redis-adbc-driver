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
  no passing cases, setup errors and nonzero process exits. Upstream expected
  failures retain their own count and follow pytest's status; they are not passes.
- Remove stale per-suite evidence, continue after independent suite failures,
  sanitize credential-bearing output, and retain JUnit/JSON/log artifacts.
- Provide Make/candidate entry points and a manual Actions workflow; PR helper
  checks validate reporting and the 50-row inventory. Full new contracts are not
  claimed to be required PR checks. Describe supported backend capabilities
  explicitly; retain upstream assertions while adapting unsupported fixture DDL.

Follow-up behavior: decimal `/` returns NUMERIC with exact significant digits;
floating-point division remains floating-point. Reconcile unresolved NUMERIC
result columns after combining bound parameter rows. SQLAlchemy emits explicit
integer keys, preserves numeric/float/binary/UUID parameters, advertises supported
RETURNING and exposes Redis table metadata without PostgreSQL tablespace queries.
The R helper supports typed writes, positional binding and visible session-local
catalog objects. Dependency patches are part of the pinned recipe, not claims
that unmodified adbi/CRAN packages pass. Unsupported operations retain explicit
capability skips and separate rejection assertions.

Risks: upstream compliance suites create/drop database objects and may expose
unsupported SQL or DBI features. Use an empty disposable database. SQLAlchemy's
default requirements do not declare full Redis capability support. R package
and some Python dependency ranges remain floating; adbi and nanoarrow are pinned
in the R recipe. No driver-level transaction or uniqueness feature is in scope.
