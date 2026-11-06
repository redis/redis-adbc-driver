# Rounded-value ordered source checkpoint

Date: 2026-11-04. Parent 573f484. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The 121-name ordinary continuation passed 16 test events then stopped on
TestSQLOrderByRoundedRuns' legacy single-index work counters (root 21.831 s).
Exact rows passed. Three 500-row INSERTs now produce three immutable sources;
repairing two rounded runs on each source in both directions sorts 12 runs,
rather than the four runs expected for a single mutable index. With global
OFFSET 600 or 986 each 500-row source is fully read, so both limited queries
also repair 12 runs, with no boundary rereads. Applying the global OFFSET to each
source would return wrong rows.

Keep the original single-index expectation with one 1,500-row INSERT and add a
separate three-source fixture. Both assert the nonempty native source count,
complete exact BIGINT order in both directions, limits/offsets across rounded
runs and cursor pages, wider-window behavior for a compound key, residual
predicates and complete streamed Arrow batches. Single-index work remains four
full-order repairs and three limited boundary rereads/repairs; segmented work
is explicitly 12 repairs for each pair. No counter assertion is removed and no
compaction is forced to disguise segmented overhead.

## Validation and review

Owned strict-TLS Redis Software with noeviction and active effective 4x QPF
verified before testing. Both race families pass, zero test skips/failures;
root 5.018 s. CUDA has no applicable selected tests and is skipped. Mutating
selections run sequentially. Formatting and diff whitespace checks pass.

Reviewed exact HASH precision versus Search doubles, per-source repaired order,
global OFFSET/LIMIT placement, nonempty source identity and full stream fetch.
The implementation comment now describes immutable pinned sources rather than
legacy mixed-time reads. No production behavior, protocol or ACL change. Author
review is not independent maintainer approval. Additional source work is real:
this checkpoint does not establish performance parity, sustained compaction or
a comparative benchmark result. Full ordinary/platform/recovery, restricted
readers and public input/process resumption gates remain open.

The subsequent 106-name ordinary continuation passes 24 test events with one
explicit CLIENT PAUSE gate skip, then fails TIMETZ fixture metadata inspection
(root 6.218 s). See the [TIMETZ checkpoint](timetz-validation.md). Later cases
remain unverified by that run.
