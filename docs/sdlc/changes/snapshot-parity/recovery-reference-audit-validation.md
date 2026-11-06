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

## Issue #344: source audit and failure-only comparison

At `af0fa15c`, all three hosted workflows are terminal SUCCESS:
[Test](https://github.com/redis/redis-adbc-driver/actions/runs/42248797797),
[Release](https://github.com/redis/redis-adbc-driver/actions/runs/42248797946) and
[DuckDB](https://github.com/redis/redis-adbc-driver/actions/runs/42248797775).
Both Software architectures pass. The conditional SQL-grant failure guard skips
because the actual authorization step passes; optional measurement skips remain
skips. Hosted proxy jobs do not replace the dedicated Cluster recovery runs.
The exact revision also passes all-node AOF recovery in **37.62 seconds**, with
all phase reference totals consistent, private outcomes retained and resumed
compaction verified. Admission verifies effective QPF4, strict TLS, noeviction,
all six shard roles and initialized AOF files with appendfsync-always.

An isolated pre-publication-guard runtime (`272bbd4d`) with current test-only
audits passes three serial promotion/RDB pairs: **8.11/63.05**, **6.55/44.12**
and **7.33/47.34 seconds**. This weakens any inference from passing new-runtime
runs that the publication guard explains the original failure. The comparison
does not establish its cause.

A test-only pipeline observer now captures the exact failed native proof before
the query unwinds. Only on a mismatch, at most three times per recovery test, it
uses a fresh TLS/RESP3 Cluster connection to reread the same bucket proof and
current root under a three-second context. It preserves every original reply and
error and never replays SQL or mutates Redis. Logs contain numeric tuples and
status flags; optional identity records stay in the fixture's chaos directory,
created exclusively with mode 0600. A later healthy read is diagnostic evidence,
not proof that the original reply was wrong: state can change between reads.

The controlled self-test alters one native reply without changing Redis. It
requires the original query failure, detection of that exact pipeline, and a
healthy independent read. A pre-guard observed RDB run passes in **47.80 seconds**
with **12 matched proof pipelines and zero mismatches**. The isolated baseline
investigation is preserved at `157cd824` on
[codex/issue344-baseline-investigation](https://github.com/redis/redis-adbc-driver/tree/codex/issue344-baseline-investigation);
it is not merged into the core PR.

On the current production runtime with the observer attached, the final
controlled self-test passes in **0.46 seconds**, including mode-0600 artifact
verification. All-node AOF recovery passes in **37.66 seconds**, followed by
primary promotion in **6.75 seconds** and all-node RDB recovery in **53.50
seconds**. Each recovery matches 12 proof pipelines with zero mismatches.
The RDB gate verifies fresh completed saves with AOF disabled on every shard;
both separate progress-helper executions pass without changing original probes.
All phase reference totals and exact recovered values remain correct. The
complete Cluster selection passes 22 events with three explicit orchestrator-only
skips (probe writes, progress and recovery); those cases run separately above. These
test-only changes require their own hosted checks; the green `af0fa15c` matrix
above does not cover the newly added observer.

The original post-promotion RDB metadata failure remains unresolved. No current
observation supplies its missing numeric tuple or root identity, and successful
reruns must not be used to close that acceptance gate.
