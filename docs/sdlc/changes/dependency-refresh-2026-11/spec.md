# Dependency refresh specification

Date: 2026-11-17. Base: `b63a010d`. Status: implemented and locally verified.
See [intent](intent.md) and [plan](plan.md).

## Contract and boundaries

- Resolve current compatible Go releases using the Go module proxy; update
  driverbase and testutil together. Use Go 1.27.4 and existing module majors.
- Match Go/Python ADBC 1.13.0; update Arrow Go to 18.9.0 and regexp2 to 2.13.0.
  Keep go-redis 9.23.0, protobuf 1.36.12 and UUID 1.6.0, already current.
- Refresh precise Python floors and explicit Airflow/Ray pins using PyPI stable
  metadata. Preserve Python >=3.12 and isolated Superset/analysis environments.
  Broad lower bounds already permit newer releases; do not require a major
  integration migration merely to raise a minimum version.
- Keep Superset's SQLAlchemy <2, Arrow <19 and Flask-Caching 2.3.1 constraints:
  the [October evidence](../dependency-refresh-2026-10/plan.md) records startup
  failure with Flask-Caching 2.5.1. Superset itself remains current at 6.1.0.
- Have CI install the existing `ray-tests` and `airflow-tests` extras instead
  of repeating integration versions in workflow commands.
- Replace duplicated string-key collection/sorting with `maps.Keys` and
  `slices.Sorted`, shared across HASH inspection and lateral memoization.
  Internal callers only iterate the output; nil versus empty does not escape.
- Retain the NULL-preserving Python float converter: SQLAlchemy's processor
  moved from `sqlalchemy.processors` in 1.4 to `sqlalchemy.engine.processors` in
  2.x; using the new import breaks the isolated Superset environment. Replacing
  this four-line helper would require another compatibility branch.
- Retain the direct statement implementation: current upstream NewStatement
  still embeds StatementImpl and hides optional StatementCanceler methods.
  Retain custom ARE matching: PostgreSQL leftmost-longest rules, rune/byte
  conversion and shared timeout are not replaced by a regexp2 version bump.
- Keep generated Substrait bindings and C boundary guards. Replacing pinned
  descriptors with an older published Go module would lose schema coverage.

## Acceptance and risks

Build shared library and CLI; run unit Go tests, Software integration tests with
active verified 4x QPF, pgdiff harness tests, and focused Python core/Airflow/Ray
checks. Record failures/skips precisely. No timing or cross-platform claim.

Primary risks are Arrow ownership/API changes, regex semantics, provider API
changes and Ray worker serialization/lifetime. Existing tests cover these;
retain assertions and do not broaden skips to force an update through.
