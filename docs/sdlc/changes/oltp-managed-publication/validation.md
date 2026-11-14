# Validation and review

Date: November 13, 2026 (America/Los_Angeles). Author/reviewer: Codex; requestor:
repository user. Base: ca685806. Human review pending.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Checks

- Baseline reproduction: temporarily restored the four changed production files
  to ca685806 while retaining the new deterministic discovery-budget test. All
  seven boundaries failed the new budget: a one-row output issued two separate
  directory page requests and no discovery pipeline. Candidate source restored
  in a `finally` block before subsequent checks.
- Candidate `go test -count=1 -run '^TestSnapshotDirectoryPopulation' .`: PASS.
  Covers 0/1/255/256/257/512/513 entries, missing count, failed page, excess entries,
  a short directory, changed final cardinality, pipeline error with populated
  replies, and lost notification reply without replay. A one-row population now
  uses one two-command discovery pipeline, one notification and one final count:
  three client exchanges rather than five. Both cardinality reads are retained.
- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  PASS across all packages. Native integration cases skip without an endpoint;
  this is not Software compatibility evidence.
- Parent-owned fixture verified by the private gate before each live command:
  Redis Software with active/effective 4x QPF, six Search workers, mnp/32 and
  strict data TLS/RESP3. Tests ran serially with GOMAXPROCS=4; no other native
  benchmark or live agent used the database during this slot.
- Focused live selection: PASS (12 seconds including helper attribution).
  New regressions prove one authorized descriptor GET and one exact directory
  page during one-row publication, preserved old-reader values, skipped abort
  after confirmed SQL commit, and abort for no-op/failed SQL. Thirteen corruption
  cases introduced after pin acquisition prove the existing atomic clone still
  refuses an invalid base without mutating references, its target map or root.
- Relevant existing live selection: 53 top-level tests PASS, zero skips, 21.75s.
  Includes private-index notifications/unknown outcomes/permissions and byte
  bounds, independent publication/concurrent writers, metadata and manifest
  integrity, Arrow ingest modes/atomicity, SQL and Arrow admission/cancellation,
  no-op publication and versioned DML. Every process/client exited before the
  parent released this live slot to the next agent.
- `gofmt` and `git diff --check`: PASS.

## Review and limits

Checked correctness/lifecycle, ACL/data ownership, and intent/evidence. Native
scripts, storage version, ownership fences, pin release, index readiness and
warning-free indexed-ID proof are unchanged. The clone's atomic completeness,
reference and live-pin checks run before its first mutation; the removed client
pipeline duplicated a subset of those checks. Publication carries descriptors
only within its synchronous post-execution phase, not across statements or pins.

No actionable finding remains from this agent review. Helper attribution and
command-budget checks do not establish end-to-end native speedup. The parent
integration work records before/after native scenarios, combined-source identity
and final full-suite checks separately. No 100M ClickBench was run. Operational
logs, fixture identifiers and credentials remain private.

## Cross-review follow-up: bound root enumeration

Independent aggregate-agent review identified that removing the client proof
also removed its pre-enumeration HLEN check: the existing clone Lua checked the
root size only after HGETALL. Move the 0..256 manifest bound and actual HLEN
comparison ahead of HGETALL inside the same atomic clone, retaining the later
length equality check. This adds no client round trip and does not alter storage
format. Two new corruption cases seed 257 actual map fields, with either a
matching oversized manifest or a false small manifest; both refuse without
changing references, target map or published root.

Verified Software 4x-QPF selection `TestSnapshotCloneAtomicallyRejectsInvalidBase`,
`TestSnapshotPublicationReusesAuthorizedDescriptors` and
`TestSnapshotConfirmedSQLPublicationSkipsAbort`: PASS, including all 15 invalid
base cases. All clients closed before returning the shared live slot. The
pre-HGETALL bound was explicitly reviewed; no source-text assertion was added.
