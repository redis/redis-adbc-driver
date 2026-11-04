# Pinned aggregate cap recovery and worker fixtures

Date: 2026-11-04. Parent 0e11fd0. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 617-name ordinary continuation passes 89 test events and skips two
helper/environment cases, then fails TestSQLPartitionedGroupCapRecovery on
unpinned catalog access (root 7.497 s). The fixture replaced its pinned store with
a fresh unscoped client. Adjacent worker fixtures also edited published row IDs
and sequence metadata in place.

A context-returning plan helper retains the actual snapshot scope for low-level
calls. Error hooks are installed on a fresh source client before workers share
it; Cluster Search hooks target its coordinator node. The original pin remains
on its own store and is never redirected to a changing client. Cap recovery
retains early/late resource errors, complete restart/discard of failed attempts,
repeated finite-range splitting and exact comparison with an independent plan.
Aggregate result maps are compared with their own plan's function identities.

Sparse worker keys, exact directories and sequence upper bounds are privately
published before planning. Duplicate values across ranges, NULLs, top-K and
OFFSET remain exact; both worker grants must be released and transfer/spill
metrics remain checked. Cancellation forces one-row cursor pages and fires only
after a real nonzero cursor ID. It must close every segment cursor, release the
worker grant and remove query-local spill files.

The small ClickBench-inspired semantic fixtures publish their sparse sequence
once instead of changing a pinned counter. They require a two-worker grant.
Their unlimited reference uses all authoritative rows in an in-memory relation,
forcing serial evaluation independently of the partition reducer. Checks require
complete group counts, no duplicate projected groups, exact membership, ordered
scores at every retained rank and HAVING values. They are not a ClickBench
benchmark, loading measurement or whole-workload performance result.

## Validation and review

Owned Redis Software with effective active 4x QPF and noeviction verified before
testing; mutating selections run sequentially:

- Cap recovery and sparse workers: two race test events pass, no skips/failures;
  root 3.385 s. The cap test covers three query shapes in early/late modes.
- Confirmed-cursor cancellation plus exact aggregate selection: 16 race test
  events pass, no skips/failures; root 3.660 s.
- Final five-family aggregate/top-K selection with independent complete serial
  reference: 23 race events pass, no skips/failures; root 4.048 s.
- Formatting and diff whitespace checks pass.

Reviewed immutable fixture publication, pin/client lifecycle, discarded partial
attempts, native visibility, independent comparison, worker/cursor/spill cleanup
and exact SQL semantics. No production code changes are required. This author
review is not independent maintainer approval. Current-protocol Cluster execution
is not proved by the proxy selection.

Full ordinary/platform, restricted-reader production integration, current-protocol
Cluster recovery, process/input resumption and sustained/comparative performance
gates remain open. These disjoint selections are not a complete ordinary-suite
pass; earlier full snapshot/native results retain their source revision.
