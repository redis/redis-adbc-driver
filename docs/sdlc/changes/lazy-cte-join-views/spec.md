# Specification

2026-11-23; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

Initial join views retain qualified column metadata and original physical-field
mapping, but no copies of an in-memory relation's rows. Empty-input reasoning
and join costing must continue to use the original base relation, not these
metadata-only views. A shared CTE remains cached once with its complete rows.

At the existing scan step, copy each base row to an alias-qualified map using
the same fields and order as before. Keep all fields, including logical source
provenance, rather than introducing projection pruning. Check cancellation
during qualification. Clear the view's full input-slice reference after scanning
on success, predicate error or cancellation. Do not clear or mutate returned
matching maps, cached source rows or shared AST nodes. Physical source keys
must retain their alignment, and later join-presence tags must affect only the
owned matching maps.

Native table scans and lateral execution retain their existing paths. The
existing nullable/isolated join rules, scan predicates, needed-column
calculation and duplicate/NULL semantics stay authoritative. This change does
not fuse filters into qualification or push one alias's predicate into a shared
CTE. It removes simultaneous unused alias copies; it does not impose a byte
budget on the cached CTE, one active copy, or valid joined output.

Acceptance: the pre-fix structural test demonstrates retained copies in all
unscanned aliases; post-fix tests prove no such copies, real base-row costing,
matched-map lifetime, source/key/tag ownership, and cleanup on error/cancellation.
Pure result regressions cover self aliases, duplicates, NULLs, outer joins,
lateral and isolated joins. SF1 timing requires separate root-controlled live
validation with verified Redis Software 4× QPF and unchanged query SQL.
