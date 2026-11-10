# PostgreSQL 18 query semantics: intent

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `85509485`. Status: targeted join/whole-row slice implemented and validated; five #361 cases remain.
Issue: [#361](https://github.com/redis/redis-adbc-driver/issues/361).

Raise the curated PostgreSQL 18 comparison above 90% by resolving real query mismatches, starting with grouped joins, USING aliases and whole-row values. The recorded baseline is 574/651; at least twelve additional cases must pass without regressions. Preserve corpus provenance, the denominator and unresolved failures. This is not a claim of full PostgreSQL compatibility.

See [spec](spec.md) and [plan](plan.md).
