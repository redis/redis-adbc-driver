# Primary-key snapshot fixtures

Date: 2026-11-04. Parent 3ab519f. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 562-name ordinary continuation passes six test events and skips two
profile/environment cases, then fails TestSQLPrimaryKeyCreate on its unpinned
rejected-table check (root 3.170 s). The fixture now requires public GetTableSchema
to return NOT_FOUND after rejected CREATE statements, preserving all precise
syntax, column/constraint and NULL errors.

An omitted optional primary_key metadata field is exercised through a valid
current-format keyless table with independent NOT NULL metadata. The fixture no
longer deletes that field from a published descriptor. Key discovery, DROP NOT
NULL, NULL insertion and failed subsequent ADD PRIMARY KEY remain checked.
This is not a legacy storage migration path.

Obsolete mutable-ALTER race hooks and raw stale INSERTs are replaced with actual
private SQL work and intervening publications. A staged ADD PRIMARY KEY must
return 40001 after an INSERT of a NULL key or a different primary-key definition
wins. A staged NULL INSERT must likewise conflict after ADD PRIMARY KEY. Each
refusal leaves the current root unchanged and preserves pinned input. Private
output is reclaimed; the key race additionally checks that no final sealed index
for that private data identity remains. Fresh explicit statements independently
validate the winning definition and existing NULLs. SQL/input are never replayed.

On owned Redis Software with effective active 4x QPF and noeviction verified before
testing, the four-family race selection passes all four top-level test events,
zero skips/failures; root 6.523 s. It retains CREATE/ALTER/rename/rekey/TRUNCATE,
CTAS, temporary keys, public metadata interfaces and NULL enforcement checks.
Primary-key uniqueness remains unenforced, matching the existing SQL contract.
Formatting and diff whitespace checks pass.

Reviewed current-format metadata, exact error and publication-conflict assertions,
unchanged roots/pinned input, private reclamation and fresh explicit continuation.
No production code changes are required. This author review is not independent
maintainer approval. Full ordinary/platform, restricted-reader integration,
current-protocol Cluster recovery, resumable input and sustained/comparative
performance gates remain open.
