# Hosted atomic compaction validation

Date: 2026-11-05. Tested production/test source: 534cd6f (before the c4698f3
explicit-collector fixture correction). Status: failed; not feature/platform
parity. [Actual run](https://github.com/redis/redis-adbc-driver/actions/runs/42064051718).
See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[local snapshot/native/benchmark evidence](atomic-compaction-copy.md) and
[the next attribution work](compaction-phase-profile.md).

The manual Test run completes with successful coverage selection, helper checks
and linux-amd64-superset. The amd64 Software job fails in ordinary Go tests at
`TestSQLCommentConcurrent`: a private metadata workspace reservation receives the
16-generation capacity refusal. The arm64 Software job passes its ordinary Go
step and fails the separate enforced SQL SELECT grant step: the narrow principal
cannot initialize the snapshot catalog because it lacks EVAL. Native validation
and later steps skipped after failures do not count as passed.

The reader failure is the documented missing isolation/authorization feature.
Granting catalog write/EVAL permissions merely to make the legacy reader test
pass would bypass the required security design. Keep SQL GRANT gated, preserve
the failure, and complete staged reader-lease/principal validation before enabling
it. This record does not claim actual restricted-reader snapshot parity.

The generation failure needs a deterministic admission regression. After a
confirmed native capacity refusal, another writer can take the manifest slot
between reclamation and the one permitted reservation retry. A new focused test
fills the real ledger with reclaimable work, uses another valid leased owner to
take that slot, and requires bounded pre-execution recovery without evicting live
work, increasing the limit, cloning metadata twice or running row GC. A separate
post-reclamation lost-successful-reply case must prove that an unknown result
never repeats reservation or starts SQL/input consumption. These new cases are
now run: the competing-slot case reproduces the native refusal before the fix;
a bounded admission loop then passes all five cases plus ten repeated public
COMMENT races (60 passing events). See [the admission checkpoint](generation-admission.md).
This reproduces a mechanism, not the exact hosted scheduling history. The complete local ordinary race run at c4698f3 passes 1,945
test events, 308 explicit skips, zero failures; root 1,168.550 s. It does not
reproduce the hosted metadata-stage capacity failure and predates the new focused
race cases and phase profiler.

The current hosted workflow does not set REDIS_SNAPSHOT_URI, so these Software
jobs do not establish execution of the disposable snapshot protocol fixtures.
Software three-shard proxy tests are also not actual Cluster API routing,
failover or AOF/RDB recovery evidence. A new sequential Software snapshot step is
staged and its YAML, order and policy-verifying Make target checked locally.
It requires an actual future run; the failed 534cd6f run predates that edit.

## Subsequent be0ed4d run

The [Test run](https://github.com/redis/redis-adbc-driver/actions/runs/42074144960)
and [Release binaries run](https://github.com/redis/redis-adbc-driver/actions/runs/42074144950)
both finish with failure. Test helpers, coverage and Superset pass; amd64 ordinary
Go still fails concurrent COMMENT with the capacity-16 refusal. Arm64 ordinary
Go passes. Its native suite reports 563 passed, eight skipped, three subtests
passed and two failures: rename-close watches the legacy rekey journal, and
cross-thread stream cancellation receives unraisable native-handle warnings.
Those warnings name an unclosed Cursor and a Connection with an open Statement;
the rename test's failure path did not close either handle. The required SELECT
grant gate still fails. This does not prove cancellation itself caused the leaks.

Linux release validation reports the same two native failures. Darwin release
Go exposes missing snapshot initialization permissions for existing narrow
roles across ACL/client-name/read-only/exec-abort, Search, adopted/foreign data,
HASH tools, partitions, discovery, statistics and sweep. The distribution and
DuckDB-native checks pass; skipped later artifacts/platform steps are not passes.
The separate [DuckDB workflow](https://github.com/redis/redis-adbc-driver/actions/runs/42074145077)
passes. Reader authorization remains an implementation gap, not a reason to
broaden grants or suppress existing assertions.

At 820ccfd, [pre-clone orphan reclamation and the corrected native-close fixture](released-owner-validation.md)
address reproduced mechanisms; [selective compaction](selective-compaction-validation.md)
reduces unchanged-base copying and preserves independent private-work leases.
These local fixes do not establish the exact causes of every hosted failure.
Fresh [Test](https://github.com/redis/redis-adbc-driver/actions/runs/42083993691)
and [Release binaries](https://github.com/redis/redis-adbc-driver/actions/runs/42083993651)
runs at that source still require terminal results. Historical server/settings
and scoped local outcomes must not be substituted for current hosted parity.

The terminal amd64 job in the 820ccfd Test run again fails
`TestSQLCommentConcurrent` at its first staged publication race (line 582), with
the generation-limit-16 reservation refusal. The downloaded terminal job log
confirms this outcome while other jobs are still running. Thus pre-clone orphan
reclamation does **not** resolve the hosted capacity failure. The remaining
mechanism needs a separate deterministic reproduction; local focused/full
snapshot passes must not be used as proof that this failure is fixed.

The terminal amd64 job in [Test at 83a9dcd](https://github.com/redis/redis-adbc-driver/actions/runs/42087730073)
again fails the same first COMMENT staging race with the generation-limit-16
refusal in 0.10 s, despite the concurrent-retirement fix. Other jobs were still
pending at observation. The local 2be297e ordinary run passes that COMMENT case
within its broader sequence but is not yet terminal. The additional reproduced
mechanisms therefore do not establish the cause of this hosted failure.

Failure-only staging diagnostics now read Redis time and control records and
report bounded counts of states, live pins, distinct pinned roots, unowned
private work, old unpinned committed generations and remaining lease ranges.
They do not print UUIDs, physical keys, metadata contents, SQL, endpoints or
credentials, change Redis state, broaden permissions or mask the original
failure. These observations are not one atomic catalog snapshot. Their purpose
is to distinguish retained live work/pins from a reclaimable admission race in
the next actual failure, before making another ownership change.

The local 2be297e ordinary process subsequently reaches terminal success:
2,020 pass events, 328 explicit skips, no failures, root 1,153.736 s. The
COMMENT case passes in 1.28 s within that broad sequence. This is local macOS
Software evidence at the stated source, not a hosted/platform pass. The new
failure-only diagnostics were published at d1d2778. Production compaction
scheduling at that source remains unchanged.

## Terminal d1d2778 run and pin attribution

The [Test run](https://github.com/redis/redis-adbc-driver/actions/runs/42090859224)
is terminal with failure. Amd64 ordinary Go again fails COMMENT admission. At
failure the diagnostic observes 16 workspaces: nine committed roots, all nine
pinned, and seven live owned staging workspaces. There are 19 live pins, eight
writing pins; the shortest private-work lease has 119,949 ms remaining. No
unpinned old committed generation or unowned staging work is reported. These
non-atomic observations justify neither reclaiming a live pin nor increasing the
capacity limit. The unexplained reader pins need attribution.

Arm64 passes ordinary Go, the separate snapshot regression step and native ADBC
validation. SQL SELECT grant provisioning still fails because the narrow reader
cannot initialize the catalog without EVAL; the required final gate fails. The
TLS partition script fails before restricted-worker validation because it asks
for the legacy logical index name, which managed snapshots no longer use.
Later Cluster and Software-proxy TLS steps are skipped. These are separate
implementation/fixture gaps, not evidence that restricted readers work.

A local non-race run of the 137 top-level tests through COMMENT passes. Its
pre-race observation has three live partition-set pins sharing one root; it does
not reproduce the additional hosted pinned roots. Inspection found that
`TestArrowResultLayouts` never released its successful live partition sets.
A new assertion reproduces that fixture leak in 0.16 s for `large_string`.
Each supported layout now registers failure cleanup, explicitly drops its
partition snapshot after all schema/value checks, requires the pin to be absent,
and requires reopening the descriptor to fail as released. Close semantics are
unchanged: portable partition sets intentionally survive producer Close.

The corrected Arrow layout/inventory and COMMENT selection passes 23 race test
events, no skips/failures, root 9.228 s, on owned Software 8.8.0 with strict TLS,
verified effective 4x QPF and noeviction. Its pre-COMMENT observation has zero
live pins. This fixes a fixture leak retaining one root, not the unexplained
additional hosted roots. These local selections also include the pending Go
compaction-polling experiment; they are not an exact d1d2778 production rerun.

Test-only hooks now associate observed acquire/partition-retain tokens with the
top-level test, SQL/inspection pool and operation. Successful releases remove
the association. Diagnostics match only currently live catalog records and print
aggregate origin labels plus separate reader/writer/partition counts; tokens,
keys and values remain private. Unknown replies are marked, never retried by
the observer. No production ownership, capacity, permission or storage behavior
changes. The next actual hosted failure must establish which origins remain.


Review of the observer at 11213c8 found an inherited `snapshotHook` pipeline
method: although its command observation was passive, pipelines could traverse
the normal snapshot fencing hook twice. The correction implements explicit
pass-through Dial/Pipeline hooks and removes that embedding. Do not treat the
11213c8 observer as proof of unchanged pipeline behavior. This is test-only;
production fencing and permissions remain unchanged. The diagnostic hosted run
at 11213c8 remains source-labeled and is not canceled or silently replaced.


With explicit observer pass-through and production compaction scheduling restored,
the Arrow layout/inventory and COMMENT race selection passes all 23 test events,
no skips/failures, root 11.344 s, on the same verified Software fixture. The
pre-COMMENT live-pin/origin counts are zero. This is a focused local check, not
an explanation of the still-active 11213c8 hosted run's retained reader roots.


## Ray workload reproduces the retained groups

The terminal amd64 job at 11213c8 distinguishes all eight unexplained pins as
live partition groups, with nine COMMENT writer pins and eight owned workspaces.
The old Ray suite immediately before Go creates exactly eight unreleased plans
on seven roots. The [Ray release checkpoint](ray-plan-release-validation.md)
reproduces the complete sequence locally: 17 passing old Ray tests leave those
groups, then COMMENT fails with the same counts/capacity. Explicit datasource
release plus test cleanup passes 20 Ray tests, leaves zero groups and makes the
same COMMENT case pass. Live capabilities remain protected. This resolves the
local reproduction; a fresh hosted pass remains required. The 11213c8 observer's
pipeline-hook caveat and all actual source labels remain applicable.

At [4e0899b](https://github.com/redis/redis-adbc-driver/actions/runs/42103905305),
AMD64 ordinary Go now passes after the Ray capability cleanup; ARM64 ordinary
Go also passes. Both separate snapshot and native ADBC steps pass. The overall
run is nevertheless terminal failure: both restricted-reader grant gates fail
on catalog EVAL permission, and both TLS partition scripts query the obsolete
logical index name before worker validation. Later Cluster/proxy checks skip.
This closes the reproduced hosted ordinary admission defect, not platform or
reader parity. Current pre-copy-index results retain source 50d6b84 and need
their own full ordinary/hosted verification.
