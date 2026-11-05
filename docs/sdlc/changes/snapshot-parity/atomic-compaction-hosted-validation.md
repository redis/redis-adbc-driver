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
