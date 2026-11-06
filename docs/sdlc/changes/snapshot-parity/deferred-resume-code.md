# Saved operation-resume prerequisites (deferred WIP)

This branch is preserved for a follow-up and is not part of atomic snapshot
PR #284. It does not implement or expose public operation resume. Do not merge
it as a prerequisite for the user's minimal core snapshot deliverable.

Base: `272bbd4d`. The saved capture-completion checkpoint is `653f8381`; the
additional reserved-row-ID experiment is `d0401fc1`. Operational artifacts,
endpoints, credentials and licenses are excluded.

## Reusable implementation

- `go/snapshot_import_capture.go` persists capturing/checking/ready phases and
  validates membership in bounded pages before output. Unknown completion replies
  abort without restarting external discovery.
- `go/snapshot_import_spool.go` preserves exact typed/binary input and uses the
  completed capture; its reserved-row-ID experiment derives stable ordinals from
  total/remaining counts instead of allocating IDs again for each page.
- `go/snapshot_write_batch.go` couples images, capture consumption, remaining count
  and durable outcomes, and checks source-order/ordinal bindings in the experiment.
- `go/snapshot.go` rejects incomplete capture publication and bypass mutations.
- Capture/admission/stream/recovery integration tests retain failure, boundary,
  exact-value, permission and persistence cases. See the
  [capture record](import-capture-completion.md) and [resume design](import-resume-design.md).

## Validation boundaries

At checkpoint `653f8381`, the completion barrier has full snapshot race evidence
(535 pass events / 39 explicit skips), existing import/adoption/provenance races,
native snapshot checks, actual-principal permission/RBAC checks, and actual
Cluster promotion/AOF/RDB persistence observations. These prove private capture
behavior, not cross-process ownership or a public API.

An initial row-ID reservation implementation passes the focused Cluster capture
selection, including partial output followed by a new private spool handle.
The final strengthened source-order/missing-reservation guards and added negative
cases at `d0401fc1` were compiled for Linux/AMD64 but have not been run as a complete
final test selection. Earlier barrier results do not validate this additional code.
This experiment requires further correctness/ACL/fault review before reuse.

## Still unimplemented

Independent retained operation ownership and takeover fencing; persisted semantic
metadata dependencies and index plan; terminal-outcome retention through GC;
SQL/Arrow capture and continuation; native status/resume/abort handles; process
restart, stale-owner, expiry and lost-publication-reply proofs. New owner states
require an explicit protocol fence rather than assumptions about old collectors.
Ordinary workspace abort/expiry still reclaims this branch's private state.
