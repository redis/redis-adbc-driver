# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `42f6e0b1`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

Add PostgreSQL 18 NORMALIZE and UNISTR for SQL/Arrow users under #110. Preserve UTF-8 text, NULLs, exact errors and parameter lifetimes. Broader string functions, IS NORMALIZED predicates, server encodings and the remaining #110 surface are outside this change.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
