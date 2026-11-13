# Intent

November 13, 2026. Requested by the user: investigate the remaining performance
areas with subagents and run benchmarks. This track measures an existing API
choice; it does not change the driver or publication defaults.

Determine whether one explicit Arrow input stream containing small batches can
amortize managed publication work while retaining small input batches. Compare
ten 1K calls, one stream of 1K batches, and one large batch for the same 10K rows
with one/four callers. Preserve all outcomes and independently validate results.

See [contract](spec.md), [plan](plan.md) and the eventual validation record.
