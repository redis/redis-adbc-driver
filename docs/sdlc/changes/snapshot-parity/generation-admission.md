# Bounded generation admission reclamation

Date: 2026-11-04. Status: implemented; focused validation passed.
Parent: c0abd76. PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

The hosted three-shard AMD64 run recorded in
[the full checkpoint](full-815-validation.md) failed concurrent comments at the
16-generation limit. A deterministic local regression establishes the underlying
admission gap: acquire a writer pin, fill the remaining 15 ledger slots using
real reservations, then abort those workspaces. An initial best-effort sweep
cannot prevent subsequent pressure. The old reservation refuses even though
retired work is immediately reclaimable. This fixture reproduces the mechanism;
it does not establish that every hosted failure has the same cause.

Before any SQL execution or Arrow consumption, a typed Redis server refusal
with the exact generation-limit message now permits one manifest retirement and
one reservation retry using the same workspace ID. Transport errors, unknown
outcomes and other refusals do not enter this path. The reservation retains its
no-retry command wrapper. Admission does not collect buckets, row HASHes,
directories or Search indexes. It preserves caller cancellation and uses the
existing reference/lease checks with the execution scope cleared. Explicit sweep
still reports only its own confirmed manifest reclamation and retains the same
subsequent metadata/data collection steps.

The hard limit remains 16. Active work, pinned/current generations and compaction
owners remain protected. If no eligible manifest can be reclaimed, admission
retains its refusal. Another writer can consume a freed slot before the single
retry; this remains bounded backpressure. SQL and input are never replayed.
No storage protocol, ACL grant, reader lease or quota changes are included.

The three regression cases cover reclaimable pressure (two reservation attempts,
one metadata clone and no data-GC claim), active-work protection, and a lost
successful reservation reply containing the refusal text (one attempt, no clone,
server-side reservation actually present). All use real Software reservations;
they do not forge catalog records or enlarge limits.

## Evidence

The pre-fix focused command failed
`TestSnapshotGenerationAdmissionReclaimsRetiredWork` with the expected generation
limit refusal. The first post-fix focused race run passed all three tests.
Both used the owned Redis Software fixture: Redis 8.8.0, three primary shards
behind its Software proxy, verified active/effective 4x QPF and noeviction.
This is not direct Cluster API, multi-node failover or persistence-recovery proof.
Private logs retain complete command output; operational identifiers are omitted.
The final related race selection passed 34 test events with zero test skips or
failures; root package 11.272 seconds. The CUDA support package had no applicable
tests. Selection: `TestSnapshot(GenerationAdmission|Sweep|CapacityAndMissingIndex|Metadata|DMLDoesNotCopy)|TestSQLCommentConcurrent`.
The command was `make -C go test-software` with `-race -json -failfast` and that
selection, with `REDIS_SNAPSHOT_URI` set to the owned test endpoint. `git diff
--check` passed. The change has not yet received a fresh full/platform run or a
hosted rerun proving the original CI failure resolved.

Full feature/performance parity remains incomplete. Reader authorization,
process/input resume, sustained compaction, current platform/recovery coverage and
matched current-source performance comparisons retain their separate gates.
