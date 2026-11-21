# Intent

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Base: `e11653eb`. Status: implemented; local verification and self-review complete; maintainer review pending.
Issues: [#481](https://github.com/redis/redis-adbc-driver/issues/481),
[#484](https://github.com/redis/redis-adbc-driver/issues/484),
[#559](https://github.com/redis/redis-adbc-driver/issues/559).

Match PostgreSQL 18 DEFAULT grammar and preserve a retained literal's resolved
input identity during ALTER TYPE. Independently isolate native Search membership
for NUL-bearing application keys, document the observed limitation, and prove
partition reads agree without changing application data or indexes.

This change does not fix Redis Search internals, infer membership from HASH
fields, add a read fallback, or claim arbitrary NUL-key inclusion by filtered
application indexes. Existing benchmark provenance remains unchanged.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
