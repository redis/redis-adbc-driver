# Validation

2026-11-23; base `d68f9a21ee7a4b5a3572926ed29870c61ceb984e`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

The canonical Q19 plan test fails with the original AND-only hook and passes with the new hook. Unit tests cover unsafe-function/cast/subquery/parameter/error fallback, missing common conditions, NULL keys, duplicate matches and outer joins.

The tiny Redis Software Q19 regression returns exact revenue `430.0000` across all three branches and excluded/NULL shipping cases. The earlier focused join selection passed nine top-level tests including outer/natural/grouped and NaN joins, with verified active 4× QPF (six Search workers, mnp routing, 32 connections). Current-main isolated-checkout unit tests also pass.

A separately built Linux ARM64 library contains only the release base plus this optimizer patch; unrelated local edits are excluded. The user-authorized SF1 Q19-only follow-up is in progress. No full benchmark suite is running; no SF1 latency or timeout-resolution claim is made yet.

Review considered NULL/duplicate semantics, full residual preservation, error/volatility fallback, type/collation restrictions, AST ownership and outer/isolated boundaries. Human review remains pending.
