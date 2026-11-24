# Specification

Date: 2026-11-23. Status: local candidate.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

The existing shape proof permits only fact-owned column keys on every star edge.
Resolve each dimension bucket once against the original fact. If a key is NULL,
missing or unmatched, return without allocating an output row. Qualified
immutable dimension maps cannot change another edge's fact-owned input key.

Traverse the selected buckets in original dimension and bucket order, preserving
the complete duplicate Cartesian bag. Allocate a full map only at a successful
leaf: copy fact values first, then each selected dimension row in original order,
so later right rows retain their exact overwrite behavior. Preserve all typed
Values, NULLs and requested hidden provenance. Never mutate fact or dimension
maps, cached CTE rows, ASTs or shared buckets.

One probe scratch object belongs to each worker for its lifetime. Reuse only
bucket-reference and row-selection slices; output maps must never reference
those slices or share mutable map storage with another leaf, fact or source.
Emitted maps can outlive scratch reuse. Keep cancellation and immediate callback
error propagation at recursive boundaries; preserve existing backpressure,
source-error precedence, source validation, iterator/grant lifecycle and reducer
ownership. No source scan, visibility rule or parallel admission change occurs.
