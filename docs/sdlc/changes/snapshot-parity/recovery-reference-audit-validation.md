# Recovery reference model and checkpoint progress

Status: test/orchestration-only follow-up to the core publication guard. Production
code is unchanged from `0f707f34`. The original intermittent RDB metadata failure
remains unexplained; none of the observations below closes it.

## Stronger serialized reference diagnostic

The recovery fixture disables background compaction and serializes writers. At
its phase boundaries, a bounded test-only model enumerates catalog root maps,
sums each known bucket's root references, adds recorded construction-source
edges, and compares those totals with the graph counters. This can catch a
positive-but-too-small counter before a collector deletes a referenced bucket.
It is not a general concurrent production verifier or a complete value/data-edge
audit. Unmapped zero-reference buckets awaiting collection are outside its scope.

Bounds are 1,024 catalog fields, 32 root records, 256 mapped buckets per root and
the existing three-second phase-audit deadline. Count checks precede enumeration.
Errors report only numeric totals, not resource identities or source values.
The bounds are diagnostic limits, not a storage-format/admission change.

The controlled Cluster self-test first accepts healthy shared references, then
decrements a shared bucket count while leaving it positive. The model rejects
the planted undercount; the test restores the count before cleanup. It passes in
**0.34 seconds**. Real promotion passes in **7.56 seconds**: all phase checks
match 11 known bucket totals across three roots before installation and six
totals across three roots after publication. No production mismatch is observed.

## Checkpoint-gate liveness and retained failed attempt

The first RDB attempt with that model stops at the fresh-checkpoint gate after
its deadline, before killing/restarting any node. Its checkpoint-only subprocess
passes; the paused main recovery test does not complete. This is retained as a
failed admission attempt, not a successful recovery or another metadata failure.
Post-attempt reads find clean shards with successful saves/AOF disabled, but do
not identify the exact gate condition at timeout. They do not establish its cause.

An independent offline case reproduces one starvation mode: a periodic RDB save
can complete in the original probe second, leaving a clean shard with no reason
to create the strictly later checkpoint required by the conservative gate. The
old orchestrator fails that case. The correction writes separate progress keys
on the original routed primaries after the freshness bound, at most once per
five seconds. The suffix preserves each original hash tag. It never changes
original probe values or their expected recovery record and does not lower the
strict freshness/AOF/completion checks or extend the 90-second deadline.

A failed, timed-out or unavailable progress command stops admission rather than
being swallowed/replayed by the readiness retry loop. Timeout exceptions are
sanitized because subprocess arguments contain private connection environment.
Future gate timeouts report only cached, sanitized per-shard persistence flags
and the save-time delta, with no extra post-timeout network request or identifiers.

All **five offline tests pass**, covering the original freshness/completion
checks, clean-shard progress, no replay after helper failure and timeout privacy.
The old-code run retains one expected failure in the three-test inventory before
the helper-failure/timeout cases were added.

## Actual RDB follow-up

With verified Software QPF4/TLS/noeviction and the same owned three-primary /
three-replica Cluster, the corrected orchestration passes a fresh RDB all-node
restart in **48.14 seconds**. The progress helper actually executes twice, each
time checking original probe values before dirtying separate keys. Every original
sentinel recovers exactly. Committed values, private rows/outcome, metadata
isolation, resumed compaction and fresh/established clients pass. The interrupted
stream fails explicitly without replay. All reference-model phase totals match
(11 known buckets before installation, six after, three catalog roots).

These are correctness observations, not availability or performance benchmarks.
The failed gate's actual cause and the original incomplete-bucket failure remain
separate unresolved observations. The publication guard's full local/native and
terminal hosted evidence is recorded in [its report](publication-integrity-validation.md).
No deferred operation-resume or compatibility expansion is merged.
