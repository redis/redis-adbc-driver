# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `19ebcb46`. Status: implemented; local verification complete; PR review pending.
Issue: [#475](https://github.com/redis/redis-adbc-driver/issues/475).

Consider the integer, bigint and numeric signatures only, accepting implicit integer widening and integer-to-numeric conversion. Prefer exact known-type matches; ambiguous unknown/smallint calls fail with 42725, and incompatible typed calls with 42883. Unknown quoted literals take the selected input type and are validated during planning, even over empty relations. Validate calls beneath casts, IS NULL and unreachable CASE/DEFAULT branches. Infer exact integer parameter widths while leaving numeric-only parameter scale unspecified. Preserve selected Arrow types, finite numeric scale/76-digit bounds, typed NULLs and prepared binding behavior. Numeric nonfinite representation remains #362.

Primary reference: [PostgreSQL 18 function type resolution](https://www.postgresql.org/docs/18/typeconv-func.html). Validate against the pinned live reference rather than treating these requirements as a complete general overload resolver.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
