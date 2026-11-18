# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

The predicate defaults to NFC and accepts a bare form keyword. IS NOT negates the boolean result while retaining NULL. The public is_normalized function accepts one or two text arguments, with case-insensitive form names, strict NULLs, text coercion for fixed CHAR, and exact signature/form SQLSTATEs. Resolve invalid typed arguments even over empty inputs or unreachable branches. Match PostgreSQL 18 Unicode 16, including long combining runs and Unicode 17 inert starters. Parameter metadata uses Arrow strings; results use Arrow booleans. No storage, retry, ownership or normalization algorithm changes.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
