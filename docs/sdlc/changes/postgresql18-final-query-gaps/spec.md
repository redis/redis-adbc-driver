# PostgreSQL 18 final query gaps: spec

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `b14aea15`. Status: targeted corpus cases validated. Issue: [#361](https://github.com/redis/redis-adbc-driver/issues/361).

Keep immutable constant folding separate from row-dependent and volatile evaluation. Prune provably dead CASE branches. EXTRACT returns exact decimal numeric without a float intermediary; DATE_PART returns float8. MD5 returns logical text through direct results and CTAS/catalog propagation. Full PostgreSQL compatibility is not claimed.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).


Constant analysis is limited to immutable operators/casts, CASE/COALESCE and a
small pure-function allowlist. It never evaluates parameters or volatile calls
at planning time. Literal facts are query-only metadata; stored row observations
are not optimizer constants. Shared CTEs keep their boundary. This does not
implement every PostgreSQL rewrite, function-inlining or recursive demand path.

EXTRACT's unconstrained numeric result uses the existing canonical declaration
and value-resolved Arrow decimal shape, including scale-preserving window SUM.
The existing finite temporal range and numeric representation limits remain.
MD5 changes logical identity; generic string functions are not all reclassified.
