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
