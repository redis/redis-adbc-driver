# Validation

2026-11-23; base `d68f9a21ee7a4b5a3572926ed29870c61ceb984e`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

The canonical Q19 plan test fails with the original AND-only hook and passes with the new hook. Unit tests cover unsafe-function/cast/subquery/parameter/error fallback, missing common conditions, NULL keys, duplicate matches and outer joins.

The tiny Redis Software Q19 regression returns exact revenue `430.0000` across all three branches and excluded/NULL shipping cases. The earlier focused join selection passed nine top-level tests including outer/natural/grouped and NaN joins, with verified active 4× QPF (six Search workers, mnp routing, 32 connections). Current-main isolated-checkout unit tests also pass.

A separately built Linux ARM64 library contains only the release base plus this optimizer patch; unrelated local edits are excluded. The user-authorized SF1 Q19-only follow-up passed through direct Redis ADBC: warm-up 15.971 s, timed 16.552/12.556/12.327 s, median 12.556 s, all full fetches validated against DuckDB reference revenue 3083843.0578. Native DuckDB median 8.437 ms. Only part (200,000 rows) and lineitem (6,001,215 rows) were loaded/count-validated. Linux ARM64 client: four CPU quota/8 GiB; Software server container: 20 GiB, one RAM shard, database capacity 14 GiB, verified active/effective 4× QPF, TLS. No client OOM increments. Historical database capacity was 18 GiB and engines shared a worker; do not attribute an isolated speedup solely to this optimizer. Full suites remain stopped. The current-main clean PR checkout also passed the tiny live Q19 regression after QPF verification. The third DuckDB-over-Redis route warm-up timed out and is tracked separately in #599, not this direct-route fix.

Review considered NULL/duplicate semantics, full residual preservation, error/volatility fallback, type/collation restrictions, AST ownership and outer/isolated boundaries. Human review remains pending.

Fresh merged-main build b9253c82 recheck: direct Q19 validates at 3.762 s median, native DuckDB 8.577 ms, third path still times out during warm-up. AOF every second and reloaded/cache conditions differ, so no controlled speedup attribution is made. Full suites remain stopped.
