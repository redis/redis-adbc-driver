# Sweep primitive isolation and native ownership guards

Date: 2026-11-04. Parent b8810cd. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 141-name ordinary continuation stops at TestSweepRetakenPrefix on an
unscoped registry operation (root 1.276 s). This test intentionally preserves a
legacy low-level deletion-time reservation guard on an independent application
HASH fixture; no SQL descriptor is registered. Its default inspection client now
has snapshot admission hooks, which correctly reject that unpinned catalog access.

The primitive uses a separate client cloned from the fixture's verified RESP3/TLS
connection options, without copying its driver hooks. Only that client handles
the primitive registry and sweeper operation; ordinary driver/inspection clients
retain snapshot admission. Cleanup removes the fixture reservation before closing
the primitive client, including on test failure. No published native metadata,
row image, SQL table or prefix-reuse/migration support is fabricated.

The ACL fixture now checks its existing principal-setup prerequisite before
producing cleanup rows. The same denied SETUSER call still produces an explicit
skip; grants, skip conditions and assertions are unchanged. On this Software
fixture it no longer produces late raw rows before discovering that setup cannot
run. The legacy ACL sweep body still needs a current-protocol principal/ownership
fixture; its skip is not evidence of permission parity.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before each selection. Mutating selections ran sequentially:

- Isolated reservation and driver-prefix primitives: two race events pass, zero
  test skips/failures; root 1.847 s. Final selection including early ACL setup:
  two passes, one explicit SETUSER skip, zero failures; root 1.866 s.
- Separate disposable native selection: eleven race events pass, zero test skips/
  failures; root 2.492 s. Tests check reclamation admission (active references,
  missing queue ownership, wrong prefix/directory identities and malformed members),
  exact IDs, stale collectors, pinned/current rows and active writer protection.

The CUDA package has no applicable selected tests and is skipped. Formatting and
diff whitespace checks pass. These selections preserve the difference between a
legacy unmanaged primitive and native reference-owned reclamation; they are not
a full ordinary-suite, Cluster API or restricted-principal result.

The fresh 136-name ordinary continuation passes five TAG/string test events,
zero skips, then stops at TestSQLTagOlderTables on an unscoped metadata read
(root 24.377 s). Passing families cover exactness, long values, joins/DML, levels
and prefix expansion. Its unknown-level fixture still directly edits a logical
metadata key; it needs private native validation coverage without an older-format
migration path. Later selected cases did not run.

Reviewed client/hook isolation, unchanged driver admission, RESP3/TLS option
preservation, deferred cleanup order, application HASH preservation,
unchanged ACL setup semantics and explicit native ownership coverage. No
production, protocol or ACL-grant changes. Author review is not independent
maintainer approval. Full ordinary/platform, current-protocol Cluster recovery,
restricted-reader, public input/process resumption, sustained compaction and
comparative performance remain open.
