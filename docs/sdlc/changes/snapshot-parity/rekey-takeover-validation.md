# Native rekey takeover, name reuse and diagnostics

Date: 2026-11-04. Parent b9704e3. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 506-name ordinary continuation failed immediately in
TestSQLRekeyTakeoverWait on an unpinned legacy catalog read (root 1.239 s).
Four remaining recovery fixtures now exercise private snapshot ownership rather
than mutable rekey records and global rollback waits.

The takeover fixture stages a real 600-row forced rewrite, stops its owner and
expires its existing workspace control. A collector pauses immediately before
its first bounded row-deletion script, with all 600 directory entries intact.
Another connection must open within its bounded context and another collector
must finish that output while the first remains paused. The published source
accepts an INSERT, a forced rename and reuse of the old logical name. Resuming
the stale collector must succeed without changing the root or either binding;
complete renamed rows, the recreated table and ownership references are checked.
There is no need to hold published-table writes behind private-output rollback.

The earlier-rename fixture drops and recreates a logical name while its old
private rewrite is still live, then explicitly rekeys the new incarnation. The
old publication must return 40001 with an unchanged root. Its original pinned
rows remain exact, every physical identity stays distinct, private/source output
is reclaimed on release and subsequent writes affect the current binding only.
It no longer fabricates a v0.0.7 reused physical prefix: earlier storage formats
remain unsupported, with no migration path.

The diagnostics fixture distinguishes an expired statement snapshot (typed I/O
error) from changed-schema/renamed-source serialization refusal (typed I/O,
SQLSTATE 40001). Actual private INSERTs must not leak after either conflict;
public ALTER and rename occur through SQL, an independent old reader retains its
binding and NULL values, and fresh requests revalidate the current schema/name.
The DROP fixture recovers abandoned private output, retires the logical source,
retains its exact old rows for readers and reclaims them only after release. A
recreated table and its new rekey cannot reuse either abandoned identity.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before testing. Four focused race families pass, zero test skips/failures; root
4.609 s. The CUDA package has no applicable selected tests and is skipped.
Mutating selections are sequential. Compilation, formatting and diff whitespace
checks pass. These cases supplement the separate actual process-kill checkpoint;
synthetic control expiry does not measure natural lease duration.

Reviewed lease non-resurrection, explicit acknowledged boundaries, concurrent GC
idempotence, bounded waits and goroutine cleanup, unchanged roots on refusal,
complete value checks, private identity non-reuse and reference-aware reclamation.
No production, protocol or ACL changes. Removed unused mutable-rekey fixture
helpers; the shared exact-error helper remains for ACL tests. Author review is
not independent maintainer approval. Full ordinary/platform, current-protocol
Cluster recovery, restricted-reader integration, public input/process resumption,
sustained compaction and comparative performance gates remain open.
