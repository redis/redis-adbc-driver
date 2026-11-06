# Native rekey layouts and field lifetime

Date: 2026-11-04. Parent cec8264. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 518-name ordinary continuation immediately fails TestSQLRekeyDbtPattern:
its namesN helper expects table-name/~N prefixes, while the actual descriptor has
canonical snapshot storage (root 0.977 s). Positive rekey fixtures now require
registered canonical schema/UUID prefixes and matching index data identities,
with explicit non-reuse checks rather than legacy monotonic name counters.

Both dbt build/drop/rename cycles pin the previous model and the temporary build
before publication, then compare their complete exact rows after DROP/RENAME.
Decimals, binary values, NULLs, NOINDEX fields, row IDs, pushed predicates and
later INSERT/UPDATE/DELETE remain checked. Private source segments are reclaimed
only after the old pin releases. Renamed bases remain immutable as new DML adds
segments. Normal same-name and name/schema/view conflicts retain their checks.

The multi-page fixture retains complete fetch, exact row-ID/value identity,
count/sum/min/max and page-boundary checks. Forced rekey must allocate new storage;
default-off and per-connection overrides retain shared storage for metadata-only
renames. Temporary moves and reverse moves use fresh canonical storage while
preserving their schema/catalog values.

The field-lifetime fixture now creates fields through SQL and drops them through
private layouts. Old pinned images retain both dropped fields through two rekeys,
while current output has only its current columns and exact values. Releasing the
pin must reclaim original/intermediate storage. It no longer writes arbitrary
fields or pending-cleanup flags into published HASHes/metadata. This is current
snapshot behavior, not an earlier-format migration path.

## Validation and review

Owned Redis Software with effective active 4x QPF and noeviction verified before
testing. The six-family race selection passes all six top-level events, zero
skips/failures; root 10.624 s. Mutating selections run sequentially. Formatting
and diff whitespace checks pass. This is semantic/ownership evidence, not a
comparative throughput benchmark or complete ordinary-suite result.

Reviewed canonical physical identity, explicit freshness, complete row/value
comparisons, pinned generations, metadata-only options, temporary ownership and
reference-aware reclamation. No production code changes are required. This author
review is not independent maintainer approval. Legacy interrupted-job/concurrent
rekey fixtures still require restoration. Full ordinary/platform,
current-protocol Cluster recovery, restricted-reader integration, public process/
input resumption and sustained/comparative performance gates remain open.
