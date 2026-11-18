# Spec

Date:2026-11-17. Requestor:Albert Wong. Author:Codex.
Base:`5b0840ce`. Status:implemented; local verification complete; PR review pending.
Issue:[#110](https://github.com/redis/redis-adbc-driver/issues/110).

Use PostgreSQL18 xoroshiro128** plus splitmix64 seeding and Box-Muller normal sampling, with synchronized per-connection state. SETSEED(float8) accepts finite [-1,1], rejects NaN/infinity/out-of-range with22023, and returns logical void (physical Arrow string, empty non-NULL value). RANDOM_NORMAL([mean float8 [, stddev float8]]) defaults to0,1; preserves strictNULL behavior, finite/nonfinite arithmetic and PG parameter/signature errors. Incompatible typed arguments and wrong counts reject before empty input. Unknown strings convert asfloat8 during planning. RANDOM consumes the same stream. Volatile effects occur only for evaluated rows/branches and survive failed statements/rollback. Distinct sessions isolate their streams. No cryptographic-randomness or cross-platform bit-identical transcendental claim. Unseeded state uses crypto entropy. Void cannot be persisted as a table/view column.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
