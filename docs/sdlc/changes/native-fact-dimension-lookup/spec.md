# Specification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: local candidate.
See [intent](intent.md), [plan](plan.md).

In the existing all-inner/non-lateral planner only, the bounded disconnected
stable-dimension rule may also postpone a proven published native managed fact.
Require an active statement pin, valid immutable managed descriptors, no owned
private segment, no live/unmanaged/opaque/cleanup source, and both physical and
filtered estimated source counts above maxUnionTerms (1000). Keep the original
first-input choice. Native targets use a conservative 4096-row product estimate;
deferred UNION targets retain 1000. The planner tracks/saturates at 4096, while
every independently generated equality key set retains its 1000-value cap.
Operation/write/maintenance/shared-pin/retained/direct/partition-executor scopes
also refuse native admission. Small/selective, private, unpinned, unsupported or
over-product targets preserve
the existing order. Tighten native dimension stability to the same published
source proof. Larger selected cohorts can conservatively prevent postponement;
this is not an unconditional Q8 execution claim.

Native lookup now combines every independently valid bounded equality union,
not merely the first eligible pair. Keep original unionQuery encoding/validity
fallback, NULL exclusion, full original/residual pointers, actual composite hash
probes and duplicate/correlation rechecks. Skip only an unsafe/over-cap pair.
Conservatively skip integer Search equality outside [-2^53,2^53], retaining full
join fallback rather than asserting a new numeric-index proof. A proven empty
NULL-only key set still suppresses right matches through the existing join.
No snapshotIndexable, metadata, public API, outer eligibility, full benchmark,
DATE/CAST predicate, batching, or parallel execution changes are included.
