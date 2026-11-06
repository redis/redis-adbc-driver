# Native TRUNCATE storage and atomic publication

Date: 2026-11-04. Parent fd2a006. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 194-name ordinary continuation passed nine events, then stopped at
TestSQLTruncate's legacy table-name prefix prediction (root 6.251 s). Its storage
assertions now require canonical registered native identities and explicit
non-reuse, rather than mutable name counters and released-prefix generations.

One old reader retains the complete four-row table, renamed/added columns,
NULLs and view output through the first TRUNCATE. Every source directory remains
intact while pinned; after release, retired rows/indexes/directories/reference
records must be reclaimed. The current empty layout is fresh, and an unrelated
table retains its physical identity and exact rows. Column/index metadata,
selective predicates, view reads and CONTINUE IDENTITY remain checked.

A real private multi-table TRUNCATE RESTART IDENTITY is staged, then a second-table
INSERT publishes. The stale TRUNCATE must return 40001 without changing the root,
either table's count, or the first table's exact row IDs. Both private outputs
must retire. A fresh public multi-table TRUNCATE, including a duplicate qualified
name and CASCADE, empties both tables in one publication. A pinned old generation
still reads the original consistent pair. Both new layouts must be fresh; their
old sources retire only after release. RESTART/CONTINUE identity and RESTRICT,
view/schema refusal, missing names and current table contents remain checked.
All validation errors must leave the committed root and both exact tables intact.

## Validation and review

Owned strict-TLS Redis Software with noeviction and verified effective active
4x QPF. TRUNCATE and the adjacent existing schema-cascade test both pass under the
race detector, zero test skips/failures; root 3.779 s. The CUDA package has no
applicable selected tests and is skipped. Mutating selections run sequentially.
Compilation, formatting and diff whitespace checks pass. This is semantic and
ownership coverage, not a full ordinary-suite or performance result.

The next fresh 183-name ordinary continuation passes 52 test events, zero skips,
then stops at TestSQLDropDuringCreateTableAs (root 29.457 s). Its legacy row-
pipeline hook never performs the competing DROP, so the writer succeeds and the
fixture reports both a missing old stale-write error and that DROP did not run.
Later cases did not run. The stale-row SQL/Arrow/temporary-write race fixtures
need native acknowledged-batch/publication boundaries and snapshot conflict
expectations. This continuation is not a complete ordinary-suite pass.

Reviewed immutable source lifetime, canonical fresh physical identities,
unrelated-table preservation, column/index metadata, exact row IDs, private
multi-table conflicts without replay, one public publication, duplicate target
names, complete pinned results and reference-aware retirement. No production,
protocol or ACL changes. Author review is not independent maintainer approval.
Full ordinary/platform, current-protocol Cluster recovery, restricted-reader,
public process/input resumption, sustained compaction and comparative performance
gates remain open.
