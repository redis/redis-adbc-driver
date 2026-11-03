# Validation: admit ordinary writes before execution

Date: 2026-11-02. Parent `faf108c`. Status: implemented and verified locally; full feature/platform parity remains open.
See the full [parity plan](plan.md).

## Problem and contract

Prior PR CI exposed ordinary-client failures during compaction publication and
at a full eight-segment lineage. A ready compactor refused new writers with
40001 before SQL execution. The reproduced gate test received that error
immediately rather than waiting. These are admission conditions, not SQL failures
that require replaying an operation.

Managed connections now wait with bounded polling at the short publication gate.
SQL DML and append/create-append ingestion also wait for their worker to reduce a
full target lineage. Full-lineage waits release the intent pin before polling a
fresh committed root; holding it would prevent maintenance from publishing.
Admission happens before workspace reservation, SQL planning/execution and Arrow
consumption. Cancellation/deadline ends waiting and leaves no acquired pin.
Transport errors propagate; no uncertain operation or SQL/Arrow input is replayed.

Immutable input proofs are shared by pointer between the admitted read snapshot
and its private workspace. No sync.Map is copied after use. Changed descriptors
still receive separate validation. Root CAS conflicts, resource limits and direct
stores without a background worker retain their explicit errors. This does not
implement independent metadata publication or raise the segment bound.

## Verification

Same owned Software proxy fixture as the [read checkpoint](validation.md): three
primary shards, verified TLS, active verified 4x QPF, noeviction. Shared database
suites ran sequentially. Native library built with `make build`.

- Gate regression: writer waits until admission reopens, then acquires; a timed
  waiter returns TIMEOUT and leaves no pin. The pre-fix test reproduced immediate
  40001 failure.
- Full-lineage regression: start at the hard bound, run a real background worker,
  issue 20 ordinary UPDATEs without a caller retry loop, and verify exactly 20
  row-image writes plus independently expected SQL values/counts.
- Writer-intent test still proves maintenance yields to an existing writer and
  resumes after release. Its old immediate-error expectation was updated to wait
  and timeout; the actual ownership assertions remain.
- Validation-cache regression also checks that the workspace reuses the admitted
  immutable input proof while rejecting a changed descriptor.
- Native snapshot suite: all 11 checks passed in 12.607 seconds, including 20
  independent Arrow appends with no caller retries and complete exact-value fetch.
- Native IPC module: all nine tests passed in 1.55 seconds. Late malformed input
  now asserts atomic create rollback and preservation of existing rows on append,
  replacing the old partial-batch expectation.
- Unit packages: 892 pass events, 661 expected integration skip events, no failures.
- Complete snapshot race rerun: 78 pass events, 7 expected Cluster API skips, zero failures; 100.928 seconds.

## Review and remaining work

Author self-review inspected admission-before-input ordering, intent-pin release,
cancellation, heartbeat/pin cleanup, shared proof synchronization, ordinary error
propagation and IPC atomicity. This is not maintainer approval. The original
publication, compaction lease/phase and input ownership fences remain.

CI failures involving legacy raw-metadata/ownership assumptions and native wide
fetch still need reconciliation and full validation. Full feature/platform and
Cluster API/recovery runs, incremental metadata/publication, resumable large
operations, compaction throughput, broader comparative benchmarks and owned
cleanup remain required. No full feature/performance parity claim is made.


## Deadline classification follow-up, 2026-11-03

During the private import-index policy validation (parent 7e09655), a complete
snapshot race run produced 140 pass events, seven expected Cluster-only skips
and one admission-gate failure in 136.426 seconds. Its 30 ms context deadline
surfaced as a raw socket I/O timeout during acquisition. Three immediate focused
repetitions passed, which did not establish that the intermittent defect was gone.

A deterministic regression then returned a socket timeout from acquisition after
the context deadline, using an internal store without the client error hook.
It reproduced the wrong I/O status with exactly one acquisition attempt. The
control case, an independent socket timeout before the context deadline, retained
I/O status. Production clients normally install errorHook; internal stores must
still preserve acquisition's context error contract without relying on that hook.

Acquisition now uses the existing transportContextError helper before wrapping a
non-admission-refusal error. A deadline becomes ADBC Timeout; an independent
socket timeout remains I/O. Neither error is polled/replayed, and no pin is
allocated or leaked. Only known compaction admission refusals remain pollable.
No timeout constant or assertion was relaxed.

The deterministic two-case regression and the original gate-wait test passed
under the race detector: two top-level cases, two classification subcases, no
skips/failures, 4.132 seconds package time. The complete rerun is recorded with
the import-index checkpoint. This focused fix does
not prove all cancellation, failover or network-outcome scenarios.
