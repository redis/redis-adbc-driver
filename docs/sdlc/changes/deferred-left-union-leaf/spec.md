# Specification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Local candidate.
See [intent](intent.md), [plan](plan.md).

Existing single-consumer UNION ALL, identity conversion, pure projection, and
no order/limit/group/WHERE/volatility/correlation guards remain. A newly admitted
leaf has exactly two direct stable sources, one explicit LEFT join, no merged/
NATURAL/USING/lateral/isolated/grouped namespaces, and no prior pushed/extra
conditions. Every ON conjunct is a direct-column equality crossing the two
sources with identical exact integer/DATE types. Native sources require pinned
valid managed published descriptors; private mutable, unpinned, unmanaged/live,
cleanup, and opaque sources refuse without reads. The second source must retain
its compiler-proven nullable status and the preserved source must not be nullable.

During each leaf run, substitute the same conservative consumer predicate as
before, clone the join/items, and rebuild only proven preserved-left pushdown.
Keep nullable-side constraints in the complete post-join residual. Never mutate
the shared original join or its conditions. Existing join execution preserves
all compound-key hash probes, matching multiplicity, NULL key nonmatches,
matched NULL values, and unmatched-left null extension. No conversion to INNER
join or right-side filtering is introduced.

The bounded streaming UNION row-bound helper continues to reject join-bearing
leaves. This narrower deferred execution may still materialize the filtered
join output and build/read the right source through established execution paths;
it is not a right-side memory or runtime guarantee. Unsupported error-capable
and volatile projections retain eager behavior. Retained execution keeps its
existing materialization and cumulative join/output admission.
