# Contract and acceptance

Original candidate: at least 64 output columns, row ceiling at least 64, no byte
budget, and supported logical/physical primitive builders. Stage at most 64
complete coerced rows before AppendValues. Preserve required-input decode order,
NULL/default/exact integers, NaN, timezone fitting, coercion/error order, aliases,
public record boundaries, OFFSET/LIMIT final-page validation, cancellation and
retained Arrow ownership. Unsupported schemas and optional byte budgets retain
the full existing scalar path. Managed and explicit unmanaged share eligibility.

A later-column coercion error must not flush a partial row; terminal release
clears strings staged before the failure. Independently retained earlier records
must remain valid. No public option, retrieval, protocol, publication or ownership
change. This copies into Arrow buffers and is not a zero-copy path.

Acceptance required complete-field correctness plus consistent native benefit
without a clear memory regression across both modes, one/four readers and
512/4096 row ceilings. The exact original eligibility and block size were frozen
before timing and not changed afterward. Higher allocation counts and disjoint
higher RSS distributions failed the memory gate. The production path remains
identical to baseline; source is preserved as an unapplied patch.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
