# Dirty descriptor reuse plan

See [intent](intent.md) and [spec](spec.md).

1. Preserve a clean baseline native library and baseline fixed-write attribution.
2. Reproduce the redundant descriptor request with a read-refusal regression;
   separately verify hydration/freshness and corruption/refusal contracts.
3. Extract unchanged local metadata policies and reuse the decoded private
   descriptor while keeping full snapshot validation.
4. Run focused checks, complete snapshot races, actual Software RBAC, unit
   packages and native C/ADBC validation appropriate to the shared loader.
5. Build a versioned candidate and measure matched public UPDATE/no-op and
   Arrow/small-batch operations in both revision orders with full verification.
6. Publish source-specific evidence and a focused PR for #336. Preserve private
   operational artifacts and remove only the owned disposable fixture.
