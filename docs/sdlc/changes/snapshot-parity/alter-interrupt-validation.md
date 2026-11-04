# ALTER interruption, lease recovery and connection shutdown ordering

Date: 2026-11-03. Parent dc6d3c5. Protocol remains 15; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Actual Close race reproduced

The prior ordinary failure never triggered cancellation because it depended on a
legacy mutable rewrite hook. Replacement fixtures pause a real snapshot source
read after the first 256-row private output batch is durably acknowledged. They
install hooks on the execution's own client before it starts; they do not mutate
a shared connection's hook chain. A successful source read is returned unchanged
when cancellation/Close occurs, so production code must handle the real context.

Both caller cancellation and StatementCancel passed, preserving published
metadata and all 600 rows. ConnectionClose exposed a separate production race:
it closed the ordinary maintenance client before the running ALTER's deferred
abort completed. The operation reported CANCELLED rather than INVALID_STATE,
and its workspace remained staging instead of immediately reclaimable.

Execution transports now distinguish socket interruption from completion of the
owning operation. The store registers executions under the same mutex as its
closed flag, after stream-owner registration. Close cancels them, closes readers
and joins operation cleanup while the maintenance client is still available.
Normal operation/reader completion unregisters the execution; asynchronous socket
teardown alone does not. The join respects the caller's context and has a
five-second upper bound. An uncooperative input cannot leave that join unbounded;
Close reports a timeout and closes resources. Failed cleanup can still require
normal lease-expiry recovery; the timeout is not a successful-cleanup claim.

Context-canceled command errors retain a ConnectionClose INVALID_STATE cause and
the original diagnostic text/details. Successful publication remains success,
and indeterminate-publication IO errors retain their operation ID and warning
against blind retry. No SQL, Arrow input, row write or uncertain mutation is
replayed by this change. All descriptor, writer-fence and publication protocols
are unchanged; no persisted ownership format or new Redis command is introduced.

## Lifecycle and paging coverage

- Caller-context cancellation and StatementCancel interrupt a rewrite after an
  actual acknowledged private batch. Published metadata/rows stay exact, reader
  leases disappear, private work becomes reclaimable, and new SQL inserts work.
- Close interrupts the same boundary, returns INVALID_STATE to the operation,
  joins its abort/pin release before closing the maintenance client, and permits
  immediate private cleanup and subsequent writes from another connection.
- Lease recovery preserves all fields of the current work record while expiring
  its lease through a trusted raw fixture. An independent collector reclaims the
  256-row private output before the old operation resumes. Its next mutation
  fails the server fence; neither row resurrection nor publication is allowed.
  Fresh INSERT and ALTER then succeed. This replaces legacy rekey TTL/owner-key
  assumptions without extending support to earlier storage formats.
- The retained partition reader-registration race now invokes Close on another
  goroutine and waits for cancellation before returning the real read result.
  Close must join that unwound operation; running it synchronously inside its
  own Redis hook would ask it to join itself. No reader may escape after Close.
- A 20,000-row conversion counts actual immutable-directory fetch pages/rows,
  verifies exact converted results, indexed filtering/ordering, dropped-field
  removal, physical release and empty pending-cleanup metadata. Managed rows
  share one catalog slot; this does not prove row distribution across shards.
- The old mutable-cursor fixture is replaced with an intervening public SQL
  update plus reference GC during a paused private copy. The stale copy must
  conflict; a fresh combined DROP COLUMN/type rewrite must publish exact results.
  The original pinned 20,000-row directory must remain exact through cleanup and
  later reclaim after its final pin releases. Published fields/indexes are never
  mutated to manufacture document-ID churn.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites are sequential on macOS ARM64. No ClickBench run or reload. Operational
logs, credentials and infrastructure details remain private.

- Before production fix: context and StatementCancel pass; Close fails with
  CANCELLED and an active staging workspace (0.38 seconds for that case).
- Corrected initial interruption checks: both cancellation modes and Close pass.
- Extended interruption/expired-lease/retained-registration selection: four
  top-level cases plus two cancellation subcases pass, zero skips/failures.
- Focused transport/cursor cancellation race checks: 41 pass events, zero
  skips/failures, including new cleanup-order and uncooperative-input deadline
  checks. Later outcome-diagnostic preservation adds one targeted unit case.

- Full unit packages at final source: 923 pass events, 815 expected integration
  skips, zero failures. Pass-event counts can include parents whose integration
  children skip; this is not server compatibility evidence.
- Final ordinary ALTER/retained-registration race selection: 23 pass events
  (21 top-level cases plus two cancellation subcases), zero skips/failures;
  root package 42.786 seconds. Large paged conversion case: 79 actual source
  pages and 20,000 rows; 8.10 seconds for the whole test. Concurrent cleanup
  and pinned-directory case: 10.50 seconds, including stale publication rejection and complete old-row validation.
- Initial full snapshot race run: 226 pass events, 15 explicit Cluster/permission
  skips, zero failures; 302.814 seconds. This preceded the final refinement that
  preserves unknown IO outcomes while mapping only canceled commands to Close.

- Final-source full snapshot race run: 226 pass events, 15 explicit skips, zero
  failures; root package 293.152 seconds. Cluster and permission skips do not
  establish compatibility for those unexecuted cases.
- Final versioned native build completed. Rebuilt C/ADBC snapshot validation:
  13 passed in 14.073 seconds; SQL/schema Arrow ABI one passed in 0.38 seconds;
  IPC nine passed in 1.08 seconds; strict-TLS Ray 17 passed in 13.06 seconds,
  three upstream warnings and no interface skips. All mutating suites ran
  sequentially against the same verified owned Software fixture.

- Next ordinary failfast run: 81 pass events, eight capability skips and one
  failure; root package 77.865 seconds. It advances through the restored ALTER
  cases and application-index managed-copy cases, then fails at
  TestACLAppIndexReader before ACL user setup: it still requires AppIndex=true
  for an adoption that correctly reports ManagedCopy=true/AppIndex=false.
  Its source and private index permissions and snapshot reader control recipe
  need reconciliation. This is not full ordinary-suite success.

Full ordinary/platform/Cluster/recovery, complete ACL recipes, process resume
and sustained performance parity remain open. Self-review is not maintainer
approval.

## Author review and remaining limits

Reviewed source/private ownership, registered execution lifetime, registering stream
owners before exposing operations, the closed-flag race, abort/pin-release ordering, result
reader transfer, bounded waits and error/outcome classification. The known Close
race has a before/after reproduction. No published source, shared index, external
HASH or caller input is mutated to manufacture interruption.

The expiration regression proves server fencing and reclamation after a known
expired work lease. It does not establish every network-renewal or failover case.
These checks cover statement/owned-result transports; complete concurrent catalog
API shutdown coverage and the wider platform matrix remain open. Uncooperative
caller input can exceed the cleanup join budget and require later lease recovery.
No sustained-throughput or all-feature parity claim follows from these fixes.
