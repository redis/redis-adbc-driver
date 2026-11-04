# Native TRUNCATE writer and DDL races

Date: 2026-11-04. Parent fa7166e. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The preceding 80-name ordinary continuation passed 57 test events, then failed
TestSQLTruncateDuringInsertSelect/continue and its parent because its legacy
pipeline hook never invoked TRUNCATE (root 3.975 s). The fixtures now use real
acknowledged private row batches or stage real private DDL before an intervening
public publication. They require typed IO/40001, an unchanged winning root and
reference-aware private output/index reclamation, without automatic SQL replay.

INSERT SELECT retains an old two-row generation, including NULL, through
TRUNCATE and fresh writes. Every fresh row is checked for exact row ID, logical
ID and NULL note. CONTINUE preserves committed IDs; aborted private reservations
do not advance the winning counter. RESTART begins at one. The one-row late
writer likewise conflicts instead of reporting success into abandoned storage;
its retained reader and all four fresh values remain exact.

Unpublished CTAS/Arrow create/temporary CTAS must be invisible, including to the
owning temporary session. A competing create, seed and TRUNCATE wins that name;
the original writer refuses publication. Arrow append races the existing binding.
Both identity modes cover full native batches and 128-row record boundaries;
the existing input wrapper rejects repeated record/Next consumption. The batch
helper validates canonical private storage, exact directory count, and first/last
acknowledged images. Existing DROP fixtures retain their 256-row expectations.

DDL cases stage TRUNCATE versus ADD COLUMN, ADD COLUMN DEFAULT versus TRUNCATE,
multi-table TRUNCATE versus second-target DROP, and DROP versus TRUNCATE/INSERT.
Stale publication refuses; fresh explicit requests use the winner's definition.
The first target of a refused multi-table TRUNCATE keeps its rows and storage.
A stale DROP preserves the new binding, while old pinned rows survive the fresh
explicit DROP and retire only after release. New fields/defaults remain indexed
and queryable. Legacy UPDATE fixtures still need conversion; their shared old
helper is temporarily located with those fixtures, with no changed assertions.

## Validation and review

Owned strict-TLS Redis Software with noeviction and active effective 4x QPF
verified before tests. Mutating selections run sequentially. The initial public
INSERT selection passes three race events (two identity subtests and parent),
zero skips/failures; root 4.685 s. The subsequent combined writer/shared-DROP
selection passes 25 race events, zero skips/failures; root 31.160 s.

Reviewed exact identity/NULL values, native acknowledged outcomes, CTAS visibility,
private cleanup, old-reader lifetime, competing DDL atomicity, public input replay
prohibition and unchanged server fences. No production, protocol or ACL changes.
Author review is not independent maintainer approval. Complete ordinary/platform,
current-protocol Cluster recovery, restricted readers, public process/input resume,
sustained compaction and comparative workload performance remain open.

The broad selection then passed all 31 intended converted TRUNCATE/shared-DROP
test events before reaching the pending UPDATE legacy fixture. That fixture and
its first subtest fail because raceDrop never publishes the competing TRUNCATE;
root 45.057 s, zero skips. This is not a production lost-write reproduction and
is not a green broad-suite result. A failed intermediate attribute assertion
was corrected to include the required native tombstone index marker. The native
layout and driver fences were not changed.

Final explicitly scoped selection: **31 race test events pass, zero test skips
or failures; root 44.591 s**. Includes ordinary TRUNCATE, all converted native
writer/DDL cases and the shared DROP race helpers. CUDA has no applicable
selected tests and is skipped. Formatting, diff whitespace and checkpoint links
pass. The separately reproduced UPDATE legacy failure remains open; the scoped
pass does not replace a full ordinary-suite result.
