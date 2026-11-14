# Contract and predeclared acceptance

Change only wideFetchChunk 4096 to 2048. Preserve schema/projection eligibility,
exact visibility/lineage/ACL proofs, directory bounds, HMGET error/missing rules,
consumer ownership, one-page prefetch, cancellation and cleanup. Narrow generic pipelineChunk and multi-segment snapshotDeltaPage remain
unchanged. Source review found that the positional managed single-source path
also admits narrow projections: those pages change too. Include a managed
eight-column control before any timing; unmanaged narrow eligibility is unchanged.

Correctness: focused wide fetch, positional projection, exact directory and
prefetch ownership/cancellation unit/race/live suites, zero failures; any skips
must be reported. Native: actual baseline/candidate binaries, unchanged full
100K x 105 typed source, managed/unmanaged, one/four readers, output batches
512/4096, warmup plus six measured repetitions per cell, complete independent
values/types/multiplicity validation. Add managed eight-column projection with
one/four readers and output batches 512/4096, also warmup plus six repeats:
84 trials per binary, 168 total. The same full-width source/index remains loaded;
only SQL projection and expected output schema change. Retain every attempt and sample. Shared
host and ordered binary blocks are limitations. No simulated network latency
claim: this experiment is the local strict-TLS topology only.

Adopt only if all native outputs validate, memory improvement is repeatable
and no cell has a clear >3% median elapsed regression outside control variation.
Report ranges and first-batch latency. Otherwise preserve the exact candidate
unapplied as evidence; do not tune the constant or eligibility after results.
A future network latency study remains separate.

See [intent](intent.md) and [plan](plan.md).
