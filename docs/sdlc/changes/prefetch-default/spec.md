# Specification

This assessment preserves the merged prototype's stage ownership and semantics.
The producer alone discovers/fetches a page and finalizes its cursor. The consumer
alone decodes/appends Arrow. Unbuffered handoff allows the current owned page plus
at most one additional page per reader. There is no snapshot guarantee or strict
byte cap for arbitrary variable-width values. Transport shutdown interrupts
sockets and joins workers/cursor finalizers before closing pools. Connection
Close covers initial fill before a public reader is attached.

Default eligibility, if the gate passes, remains restricted to wide managed
positional HASH streams (at least 64 required columns), with no GPU execution,
direct-key plan, tie sorting or late materialization. Narrow, adopted, residual,
short pushed LIMIT and other unsupported plans retain their existing paths.
Both regular and public partition readers use the same eligibility. Preserve
order, LIMIT/OFFSET, generation/readiness checks, exact values, ownership and
current supported RESP3/TLS/routing behavior.

Matched measurements use runtime source `4735cbd` and Go 1.27.3 with prefetch
compiled off/on, never the historical pre-shutdown version. Independently verify
the restored official-source fixture and every selected typed value/NULL/row
multiplicity outside timing. Keep the full 64/105-column and narrow controls,
regular/partition APIs and one/four readers. Record actual first batches, elapsed
pairs/ranges, CPU, allocations, sampled heap and current child RSS. C-boundary
results have separate timing/allocator interpretation; Go allocation counters
are unavailable through that harness. Separate wire diagnostics do not supply
primary elapsed results. Preserve failures and regressions.

The declared elapsed rule and four-logical-CPU control are in [plan](plan.md).
Any default-selection change needs the completed evidence and tests; a positive
historical result alone does not authorize claiming that this assessment passed.
