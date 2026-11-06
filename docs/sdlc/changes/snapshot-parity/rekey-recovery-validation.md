# Native rekey recovery and concurrent writes

Date: 2026-11-04. Parent 261b4de. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 512-name ordinary continuation failed immediately in
TestSQLRekeyResumes on an unpinned legacy catalog read (root 0.980 s).
Recovery fixtures now stage an actual private forced rewrite, rather than install
mutable legacy rekey records. An intervening write makes publication return
40001 without changing the root or pinned input. Expired abandoned work is
reclaimed through both a newly opened connection and an already-open connection's
next statement. Committed output survives its coordinator; old readers retain
the original name and exact rows until release permits reclamation.

Concurrent-statement coverage checks stale INSERT versus rename/name reuse and
private rekey versus destination-view creation. Refused output cannot leak into
either binding. Lease coverage checks successful renewal, refusal after expiry,
unchanged private images, no index creation and recovery before a fresh write.
Information-schema checks use registered canonical identities and distinguish
metadata-only rename from an explicit fresh physical rewrite.

The continuous writer fixture overlaps INSERT and moving-row UPDATE with an
acknowledged private copy. It accepts only explicit serialization refusals or a
missing old binding, compares the complete result with every acknowledged write,
rejects duplicates and refused INSERTs, and checks physical output and retired
source reclamation. Additional client rename requests occur only after proven
40001 refusals; the driver does not replay SQL.

## Backpressure and limits

An exploratory requirement that writers continue acknowledging changes while the
copy remains paused timed out (root 8.988 s). Inspection confirmed existing
admission rules: eight segments bound admission, compaction installation respects
live writing pins/workspaces, and its short installation gate can hold new
writers. A paused rewrite can therefore prevent the compaction needed for further
admission. The test now requires an observed source change or a pending writer
with a verified segment-limit/active-install-gate condition, releases the rewrite,
and requires completion with exact acknowledged results. These fences remain
unchanged. This establishes bounded-admission correctness, not sustained write
throughput or performance parity. Improving that interaction remains open.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before each selection. Mutating selections ran sequentially:

- Four restored recovery/concurrency/lease/information-schema families: four race
  passes, zero test skips/failures; root 4.239 s.
- Continuous writers: one race pass, zero test skips/failures; root 5.218 s.
  Five acknowledged INSERTs, one rejected INSERT, verified backpressure.
- Combined native rekey/Close selection: 16 race test events pass, zero test
  skips/failures; root 17.238 s. Concurrent writers again recorded five
  acknowledged INSERTs, one rejected INSERT and verified backpressure.
- Existing lost-reply GC recovery: one race test passes, zero test skips/failures;
  root 1.885 s. Of 600 retired rows, the first acknowledged server batch removes
  256 despite a lost reply; a fresh collector removes the remaining 344 in two
  bounded batches starting at row 257, without persisting a SCAN cursor.

The CUDA package has no applicable selected tests and is skipped. These are
focused selections, not a complete ordinary-suite pass. The earlier continuous
writer fixture failed on an unscoped legacy registry assertion (root 3.996 s);
its ambiguous-write allowance has been replaced with strict acknowledged/refused
outcomes.

Reviewed private ownership, non-reused identities, unchanged roots on conflict,
lease non-resurrection, inactive committed outcomes, complete row comparisons,
bounded cleanup and reader-protected reclamation. No production code, protocol or
ACL changes. This author review is not independent maintainer approval. Simulated
owner expiry and fresh-collector recovery do not prove process-kill coverage or
provide a public process/input resume API. Those, full ordinary/platform checks,
current-protocol Cluster recovery, restricted-reader integration and comparative
performance remain open.
