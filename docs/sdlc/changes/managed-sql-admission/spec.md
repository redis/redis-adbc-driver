# Contract

Managed top-level INSERT, UPDATE, DELETE and MERGE join the same logical-table
admission gate as Arrow append/create_append. Wait before reserving private work
or evaluating SQL; release provisional pins and reacquire current state. Retain
cancellation/deadline/Close behavior, lease fencing and no uncertain-reply replay.
Independent targets and unmanaged writes remain concurrent. DDL, data-changing
CTEs on additional targets, external/older writers and changed managed read
dependencies can still conflict; do not promise conflict-free arbitrary SQL.
No storage-format migration, row merge or automatic statement retries.

See [intent](intent.md), [plan](plan.md).
