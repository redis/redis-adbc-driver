# Public stream cursor inspection and snapshot lifetime

Date: 2026-11-04. Parent ead1889. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 148-name ordinary continuation passed ten events, then stopped in
TestSQLStreamResults and its first child (root 8.157 s). The stream returned its
complete insertion-order rows before the cursor-inspection helper attempted an
unpinned catalog read. That helper now resolves the current table binding under
one pin. Only recognized missing-cursor errors mean the cursor is gone; other
inspection errors fail instead of silently passing a cleanup assertion. Retained-
generation tests carry their original sources instead of resolving a reused name.

The existing public stream cases retain live cursor requirements, exact rows and
batch boundaries, schema, ordering, early release, context/statement cancellation,
Close, concurrent Next/Cancel, residual global LIMIT/OFFSET, later-page errors,
explicit cursor expiry, bound parameters, materialized fallbacks and options.
UNION ALL cases retain child transitions, lazy branch opening, consumer buffers,
worker cleanup, errors and fallback behavior.

A new public lifetime fixture starts from a 25,000-row baseline plus a replacement
with NULL and a tombstone, then starts a 37-row-batch segmented stream. Its actual
metadata must use native segmented visibility and own a reader pin. After the
first batch, another connection changes an unread row, TRUNCATEs, writes, DROPs,
recreates and writes a replacement binding, then runs GC. Every original source
directory must remain intact while the stream's pin is active.

The finishing case checks every exact ID, label/NULL and int32 quantity across
all 24,999 visible rows, including replacement/deletion visibility and order.
Separate early-release, context-cancel and connection-Close cases check the same
initial images plus cleanup and typed termination. Every mode must release its
reader control, permit original rows/indexes/directories/reference records to
retire, preserve an independently retained Arrow batch through reclamation/Close,
and leave the current replacement row unchanged.

## Validation and review

Owned strict-TLS Redis Software with noeviction and verified effective active
4x QPF. The public stream and UNION ALL selection passes all 45 race test events,
zero test skips/failures; root 49.036 s. The CUDA package has no applicable
selected tests and is skipped. Mutating selections run sequentially. Compilation,
formatting and diff whitespace checks pass. This is focused Go semantic/lifecycle
coverage, not a full ordinary-suite, native platform or throughput result.

Reviewed current versus retained source identity, narrow missing-cursor error
classification, exact complete rows and physical Arrow types, batch bounds,
actual multi-segment/pin admission, unread-row changes, source retention, EOF/
release/cancellation/Close behavior, consumer Arrow ownership, reference-aware
reclamation and unchanged replacement data. No production, protocol or ACL
changes. Author review is not independent maintainer approval. Full ordinary/
platform, current-protocol Cluster recovery, restricted-reader, public input/
process resumption, sustained compaction and comparative performance remain open.
