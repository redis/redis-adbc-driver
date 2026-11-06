# Released reservation owner checkpoint

Date: 2026-11-05. Parent: be0ed4d. Implementation validation in progress.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[the admission checkpoint](generation-admission.md).

A successful reservation whose reply is lost leaves an unexpired staging
workspace, without returning its ID to the caller. The caller correctly stops
without cloning metadata or replaying the reservation and releases its reader
pin. Previously, collection retained that workspace until its separate work
lease expired, even though its absent owner could no longer publish. Enough
such work can exhaust the unchanged 16-generation limit.

The new regression creates this condition through an actual lost successful
reply, then fills the ledger with real live owners. Before the fix it fails in
0.120 s with the native capacity refusal. No catalog records or lease times are
forged. Collection now recognizes pre-clone staging reservations whose token no longer
has a live pin matching its base. Claiming atomically marks them deleting,
fencing late clone mutations. Established private work keeps its independent
lease and references, even after the base pin stops. Current roots and live pinned generations stay protected;
durable compaction retains its separate ownership and recovery protocol.

Admission still uses the same workspace ID after a definite capacity refusal,
performs no row/index collection and never retries an unknown reservation reply.
The regression requires a fresh unexpired orphan, one subsequent clone, no data
collection, unchanged root and limit, and preservation of every live owner.
The admission cases plus concurrent COMMENT pass ten race-enabled repetitions:
70 test events, no failures/skips, root 12.450 s, before narrowing reclamation
to pre-clone reservations. The first combined run exposed an existing private
descriptor retention regression: reclaiming all ownerless work revoked a
still-valid independent work lease. The implementation was narrowed instead of
weakening that test. Additional assertions protect
the still-live unknown owner and reject renewal/sealing after reclamation;
the focused combined race selection passes ten events, no skips/failures, root
4.069 s, including those assertions and the unchanged independent private-copy
retention test. Final implementation 820ccfd passes the complete snapshot race
selection (406 pass events, 24 explicit skips, root 418.647 s) and unit-only race
checks (1,097 pass events, 958 expected skips, root 54.076 s); no failures.
See [complete local evidence and limitations](selective-compaction-validation.md).

This fixes a reproduced capacity leak. It is not yet proof that it explains all
hosted concurrent-writer failures; a fresh hosted run remains required.

## Native close regression

Hosted validation at be0ed4d failed because the native rename-close test observed
the legacy mutable rekey journal. Snapshot rekeying instead writes private
bounded batches. The test now waits for an acknowledged private batch, closes
the connection, requires a cancellation/closed ADBC error, verifies its owner
pin is gone and its workspace cannot publish, and checks every original value
after an immediate new insertion. Its existing five-second close/write bounds
remain. Cleanup closes the connection and joins the thread even on an assertion
failure, avoiding leaked native cursor/connection handles.

The corrected close test and unchanged cross-thread stream cancellation test
pass together: two tests, no skips, 4.13 s, through the versioned native library.
An initial local attempt used byte keys against the decoded raw client and
failed; correcting that fixture type did not change driver behavior. Hosted
unraisable warnings explicitly named an unclosed cursor/connection; the native
rerun does not suppress warnings. The final complete native suite at 820ccfd
passes 565 tests and three subtests, eight capability skips, zero failures,
90.68 s, with the tested library hash in the linked evidence. Full
hosted/platform validation remains open.

All database evidence above uses the owned Redis Software 8.8.0 strict-TLS
fixture, verified active/effective 4x QPF and noeviction, three primary shards
behind the Software proxy, macOS ARM64 and Go 1.27.3. Native Python is 3.14.10,
PyArrow 26.0.1 and ADBC manager 1.13.0. It is not Cluster API/failover evidence.
Operational identifiers, endpoints and credentials remain private.
