# Failed reader-pin cleanup and compaction recovery

Date: 2026-11-05. Status: implementation checkpoint; full [parity](spec.md)
remains incomplete. See [Cluster evidence](cluster16-validation.md).

## Trigger and intended behavior

An all-node RDB restart can interrupt a writer pin's heartbeat renewal and its
deferred server release. The heartbeat then stops. A later explicit local release
previously only joined that stopped goroutine, leaving the persisted writer pin
alive until its 120-second lease expired. Compaction correctly refuses publication
while that intent remains, but its recovery test exhausted its 30-second budget.
An argument-free trace recorded 4,634 publication refusals with short individual
round trips; this was a busy ownership barrier, not a slow-command diagnosis.

Record the heartbeat's cleanup outcome. A subsequent explicit release retries
only a failed token release, under a five-second context. Normal successful
cleanup still sends exactly one release command. Concurrent cleanup callers join
one in-flight attempt; they cannot each queue another five-second timeout. A
later explicit call can retry a completed failure after transport recovers.

The release remains an idempotent deletion of the original unique token. No SQL,
row mutation or publication is replayed. Restricted reader cleanup still uses its
own domain control script and permissions. No storage format, retention lifetime,
namespace budget or publication admission rule changes. Cleanup remains best
effort during a continuing outage; the existing lease expiry protects safety.

## Focused evidence and review correction

- Actual Software 8.8, strict TLS, verified effective 4x QPF and noeviction.
  A deterministic transport hook makes the heartbeat's release fail before
  mutation. After transport recovery, explicit release must remove the surviving
  writer pin. Original code fails, root 0.857 seconds. The first correction passes
  ten race-enabled repetitions of this and normal concurrent cleanup: 20 events,
  root 2.223 seconds.
- Read-only review found that holding the retry mutex across I/O could serialize
  eight continuing failures into eight full timeouts. A context-blocking transport
  regression reproduces that failure against the first correction, root 7.863
  seconds. The test unblocks and joins all callers before fixture cleanup.
- The final shared-attempt implementation passes three race-enabled repetitions
  of continuing failure, later recovery, writer cleanup, reader-domain cleanup
  and normal concurrent cleanup: 15 pass events, zero skips, root 17.168 seconds.
  Eight callers share one failed retry. A later recovered call removes its pin;
  repeated successful cleanup sends no further command.
- The reader-domain subcase executes actual reader-control Lua through an
  administrator-backed transport. It is control-path coverage, not proof of an
  independently restricted ACL principal. Existing actual-principal SQL/RBAC
  coverage remains a separate gate.
- All unit packages passed on the first correction, root 26.986 seconds. This
  predates the shared-attempt correction and is not relabeled as final evidence.
- Three real RDB all-node recovery repetitions on the first correction passed:
  53.72, 47.91 and 47.72 seconds. They retain the original 30-second compaction
  completion deadline, exact primary sentinels, private row outcomes and final
  values. Later final-code checks are recorded in the Cluster checkpoint.

The existing collaborating Actions repair chat reviewed both versions read-only.
Its first important finding is reproduced and corrected; its second inspection
found no further actionable issues with lock/channel ordering, idempotent release
or permission boundaries. This is agent review, not human maintainer approval.
Final broad snapshot, native, platform and recovery checks remain required;
passing focused tests alone does not establish full parity.

Later checks include all unit packages under the race detector (root 54.395
seconds), the anchored snapshot selection (471 pass events / 61 explicit skips,
root 410.908 seconds), actual-principal SQL/RBAC (21 race events, no skips,
root 24.851 seconds), native Cluster C ABI (19 cases, no skips, 8.957 seconds),
and initialized AOF recovery (38.86 seconds). See the Cluster checkpoint for
source boundaries, failed fixture attempts and the separate unresolved RDB
metadata validation error. The full platform/performance goal remains active.

Integrated candidate `9036c90` additionally passes the 75-event race supplement
without skips, all 19 native macOS snapshot cases, full official native validation
(565 tests plus three subtests, eight skips), and the complete ordinary Go suite
(2,157 pass events / 379 skips, root 1,240.876 seconds). Exact runtime versions,
library/source equivalence, failed configuration attempts and coverage boundaries
are recorded in the Cluster checkpoint. The later workflow-only wheel-fixture
toolchain fix does not change production behavior. Hosted final-head validation,
the unexplained RDB metadata failure and the full parity obligations remain open.
