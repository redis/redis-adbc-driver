# Plan

[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Inspect the existing managed point/native diagnostic and publication source.
2. Add a small primary-routed read helper; reserve only validateSnapshotTables in
   snapshot.go, avoiding independently owned point/root and unmanaged changes.
3. Unit-check exact commands/order, NoRetry, whole-pipeline loss, composed/native
   failures, directory precedence and cancellation. Keep explicit readiness
   injection coverage for the newly pipelined first probe.
4. In a coordinator-granted exclusive verified Software 4× QPF slot, run relevant
   index readiness/publication/lost reply tests, then matching 1K-row four-column
   diagnostics before/after with other builds/tests paused. Do not infer a speedup
   from a round-trip reduction; record actual timings and complete final states.
5. Independent review, commit actual evidence/limits and return the isolated
   changes to the coordinator. Keep raw operational logs private and historical
   bundles unchanged. Maintainer review and merge remain separate.

## Selection outcome

**NOT SELECTED for integration.** The [native subset](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md)
retains all 96 records, mixed gains and material DELETE latency regressions.
The coordinator chose to omit publication batching and retain only direct-native
fail-closed safety fixes with real-validator regressions. Frozen experimental
source/timing identities and the code patch remain archived; no performance
results are relabeled as the later safety-only source.
