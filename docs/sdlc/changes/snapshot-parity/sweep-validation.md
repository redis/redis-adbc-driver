# Explicit snapshot cleanup reports and recovery

Date: 2026-11-03. Parent source 9cc18c7. Storage remains protocol 14; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Reproductions and correction

The snapshot branch of SweepReleased ran one best-effort GC step and returned an
empty success report. Its utility connection also reaped data before preview.
An actual private generation was built, indexed, aborted and reference-retired
without reclaiming its data. Re-enabling the original public sweep behavior
reproduced both failures: dry-run connection setup deleted the three rows, and
Apply reported zero after deletion. The original behavior was restored only for
that failing run, then the corrected implementation was reinstated.

Explicit sweep now acquires its normal catalog pin without connection-side GC.
Preview enumerates at most 256 registered zero-reference queue candidates,
validates their owner/prefix/count records and counts existing HASHes through
exact row directories. It never detaches references or deletes data, indexes or
metadata objects. Pin acquisition/expiry bookkeeping still writes control state.
Concurrent background GC is independent and can change observed counts.

Preview does not simulate future graph retirement. MetadataPending/metadata_pending
and More/more explicitly report additional retirement or a truncated preview.
A zero current-row count with pending metadata is not a claim that Apply has no
work. This differs from legacy released-prefix scanning; the CLI explains it.
ManagedSnapshots/managed_snapshots identifies the mode. Released/released counts
eligible data generations observed in the pass, rather than cumulative legacy
registry entries. Prefix rows and confirmed Deleted/deleted counts remain present.

Apply performs at most 256 rounds through the existing ownership/retirement
protocol, honoring the caller context and the statement maintenance timeout.
Each round retires an eligible manifest, advances bounded metadata collectors and
claims at most one unreferenced data generation. Row directory pages remain
bounded and use logical lexicographic continuation; no SCAN cursor is persisted.
Current/pinned manifests, staging writer ownership and durable compaction edges
continue to protect data. Unregistered objects are not inferred to be owned from
a matching prefix. Index drops do not use DD. Registered prefixes stay canonical
and immutable. This adds no new storage records or migration requirement.

Shared background GC uses the same one-step implementation and retains its
best-effort behavior/budget; no new per-row read is added to that path. Explicit
maintenance propagates errors and reports confirmed deletion replies and retired
manifests. An unacknowledged DEL cannot be counted as confirmed. Failed work
remains reference-owned/deleting in the durable queues; another explicit call can
resume. No uncertain SQL/input/index command is automatically replayed. More also
signals incomplete/failed passes; it does not guarantee work will still exist
when another caller resumes.

## Lifecycle and regression reconciliation

The new protocol suite verifies preview versus Apply counts, private index and
row/directory reclamation, preservation of unregistered HASH/string objects and
application indexes, pending metadata, old-reader/current-row safety, active
writer publication after sweep, a late index-drop reply and explicit resume.
Malformed directory members must surface I/O errors with partial reports and
no row deletion; correcting the member permits explicit recovery.

The previous TestSweepReleased/TestSweepSkips constructed mutable legacy metadata,
late unfenced writes and prefix reuse using raw lookups in a current catalog.
Their applicable safety requirements move to the snapshot lifecycle tests:
active staging ownership and pinned/current generations, exact directories and
unregistered/application preservation. Existing stale-writer/prefix/compaction
protocol tests remain. A separate raw owned fixture retains the legacy
retaken-prefix deletion guard with no fabricated current table descriptor.
Legacy driverPrefix parser coverage also remains.

The CLI regression uses information_schema for the physical prefix, holds a real
stream pin across DROP, then closes its worker before releasing the stream. It
checks non-destructive preview, pending-retirement text, owned-row counts in Apply,
JSON completion and unknown-object preservation. Old raw metadata and manually
fabricated unregistered rows are no longer treated as valid snapshot garbage.

## Concurrent collectors

A final deterministic overlap check exposed a spurious failure: both collectors
received the same manifest claim; the second observed its reply only after the
first finished. It then reported metadata retirement not admitted. The second
collector now confirms that both the work record and manifest map are absent,
which is the durable completed outcome of the existing protocol. It neither
replays retirement nor hides other errors or a missing map with a surviving work
record. Both calls must succeed, their confirmed deletions must sum to three and
exactly one manifest must be counted. This models overlap with automatic GC.

## Verification

Owned Redis Software fixture: strict TLS, Search, active effective 4x QPF and
noeviction reverified with make -C go redis-up. Database suites run sequentially.
Local machine is macOS ARM64.

- Original behavior reproduction: both preview/Apply subcases failed as described.
- Focused ownership/recovery race run: six top-level cases plus two preview/Apply
  subcases passed, no skips/failures, 2.420 seconds before the final partial-report
  refinements; final focused results are recorded below.
- Legacy reservation/parser race cases passed, 1.838 seconds; CLI sweep passed,
  1.922 seconds before the final error-footer/report refinements.

- Before the concurrent-collector correction, the complete snapshot race suite
  passed 152 events with seven expected Cluster-only skips in 131.805 seconds.
  Native 13, IPC nine, Ray 17 and CLI passed on that earlier source too.
- Deterministic duplicate-claim regression initially failed with metadata
  retirement not admitted. After outcome confirmation, all seven sweep lifecycle
  cases plus two preview/Apply subcases passed, zero skips/failures, 2.615 seconds.
- Final complete snapshot race suite: 153 pass events, seven expected Cluster-only
  skips, zero failures, 131.990 seconds.
- Final unit-only go test -json -count=1 -p 1 ./...: 905 pass events, 737 expected
  integration skips, zero failures. Skips do not prove compatibility.
- Final shared library rebuilt with make -C go build and its version linker flag.

- Final rebuilt native C/ADBC snapshot validation: 13 passed, zero skips/failures,
  13.387 seconds. IPC nine passed (1.08 seconds); Ray 17 passed (12.89 seconds,
  three configuration deprecation warnings), zero interface skips.
- Final ordinary legacy reservation/parser race cases: two passed, 1.730 seconds.
  Final CLI SweepCommand race case: passed, 2.063 seconds, including live-pin
  protection, pending preview, confirmed Apply counts and JSON completion.
- Changed Go formatting, local Markdown links and git diff --check passed.

No throughput or performance parity is claimed for this change.

## Review and open requirements

Reviewed dry-run side effects, canonical graph ownership, immutable directories,
zero-reference admission, pin/writer protection, metadata/data separation,
confirmed partial counts, unknown reply handling, cancellation, retained deletion
state and source ownership. Self-review is not maintainer approval.

Cleanup permission errors stop this explicit pass with a partial report. Schema-restricted
ACL sweep/skip behavior and complete least-privilege command/pin recipes remain
open; the separately named ACL family is not included in ordinary sweep checks.
An interrupted directory deletion currently restarts lexical traversal from the
beginning; already deleted IDs remain until the generation finishes. Durable
per-batch directory progress and background throughput under repeated short
budgets remain open, so safe explicit recovery is not a large-GC performance
parity claim. The 256-round continuation boundary has not been stress-benchmarked
here. Full
platform/latest Cluster/failover/persistence checks, resumable large operations,
sustained compaction and comparative performance remain required by the goal.
