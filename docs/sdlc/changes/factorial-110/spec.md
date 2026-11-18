# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

Accept smallint/integer widening to bigint, bigint, unknown integer strings and NULL; reject incompatible typed arguments before NULL/empty execution. Return NUMERIC(76,0) as Arrow decimal256. Compute 0! through 56! exactly; negative input and results beyond the existing 76-digit bound return 22003. PostgreSQL values 57! through 32177! exceed this representation and remain explicit compatibility gaps, never equivalence passes. Preserve unknown-literal errors, lazy value branches, parameter widths, input ownership and prepared reuse.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
