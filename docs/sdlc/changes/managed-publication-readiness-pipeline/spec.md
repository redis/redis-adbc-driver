# Specification

[Intent](intent.md), [plan](plan.md), [validation](validation.md).

After execution and final private-index population, queue exactly the existing
ZCARD and FT.INFO reads, in that order, on the primary selected for the registered
`{meta}` index. Retain native FT.INFO NoRetry; no uncertain pipeline is repeated.
The metadata primary selection is the same helper used by standalone Search.
The private directory is not mutated during this phase. A fresh later FT.INFO
poll remains separate, with unchanged delay and independent indexed-row proof.

Directory errors retain precedence. A pipeline error with populated replies, or
one different from the same direct native FT.INFO failure, is terminal. A wrapped
or joined native error can carry reply loss and never proves a safe retry. Only
the two exact direct native inconsistent-index-state replies permit polling.
Authorization, warnings, final prefix/cardinality, pin/lease, row ownership,
compaction queue, private metadata SET and atomic publication remain unchanged.

No cached count is introduced: the count is fetched once at the same publication
phase as before. Each new statement/output repeats it. Readiness fault-injection
hooks must intercept the first pipelined probe as well as standalone later polls.
Opt-in wait diagnostics attribute the initial batch's client wait explicitly,
not individual command server time.

## Selection outcome

**NOT SELECTED for integration.** The [native subset](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md)
retains all 96 records, mixed gains and material DELETE latency regressions.
The coordinator chose to omit publication batching and retain only direct-native
fail-closed safety fixes with real-validator regressions. Frozen experimental
source/timing identities and the code patch remain archived; no performance
results are relabeled as the later safety-only source.
