# Retained execution comparison pin

Date: 2026-11-04. Parent bf9ef89. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 623-name ordinary continuation passes 17 test events, then fails the
grouped-view child and parent of
TestRetainedExecutionGlobalSQLMatchesExecuteQuery on unpinned catalog access
(root 1.498 s). The low-level comparison helper lets execute acquire/release its
own scope, then runs final retained-definition validation outside that pin.
GETRANGE now correctly rejects that unscoped metadata access.

The helper now mirrors the public statement boundary: acquire a reader pin on
the base store before constructing the retained executor, keep it through both
execution and final validation, close the bounded source client and release the
pin before closing the statement. Resource budgets remain on the retained source
client; lease acquisition does not consume its transport allowance. No production
behavior or public option changes are needed.

On owned Redis Software with effective active 4x QPF and noeviction verified before
testing, the focused three-family race selection passes all 31 test events with
no skips/failures (root 3.191 s). It fully fetches and compares physical Arrow
values/types with ordinary SQL, including exact integers, decimals, NULLs, nested
arrays/records, aggregate/window ordering, DISTINCT, joins, sets, CTEs, subqueries,
a grouped permanent view and owned temporary sources. Separate cases retain
transport/source/working/work/join/window limits and reject session side effects.
Formatting and diff whitespace checks pass.

Review checked pin/client cleanup order, use of the production statement scope,
resource error assertions and full fetch/type comparisons. This author review is
not independent maintainer approval. The preceding continuation and focused
selection are not a full ordinary-suite pass. Restricted-reader integration,
current-protocol Cluster recovery, full hosted/platform checks, resumable input
and sustained/comparative performance gates remain open.
