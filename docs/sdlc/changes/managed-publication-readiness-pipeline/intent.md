# Intent — November 14, 2026

Requested by Albert Wong as the next performance tasks, implemented by the
managed-publication Codex subagent. Status: NOT SELECTED experiment, archived; not integrated.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

The previous native driver comparison still spends approximately 40–46 ms on
single-client managed mutations. Existing diagnostic profiles show fresh indexes
usually ready on the first poll, so faster polling is not the target. Remove a
serial publication exchange by queueing independent native directory-count and
first index-readiness reads together. Preserve every publication/ownership proof.

No storage-format or guarantee change, live-value/permission caching, SQL/input
replay, server CPU claim, capacity claim, ClickBench, Flex or transfer measurement.

## Selection outcome

**NOT SELECTED for integration.** The [native subset](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md)
retains all 96 records, mixed gains and material DELETE latency regressions.
The coordinator chose to omit publication batching and retain only direct-native
fail-closed safety fixes with real-validator regressions. Frozen experimental
source/timing identities and the code patch remain archived; no performance
results are relabeled as the later safety-only source.
