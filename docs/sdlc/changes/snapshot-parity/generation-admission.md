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

## Competing-slot checkpoint, 2026-11-05

The one-retry behavior above still permits a different writer to consume the
freed slot. A new deterministic case reproduces that refusal: fill the real
16-slot ledger with reclaimable work, retire one manifest, then use another
valid leased owner to reserve the freed slot before the original retry. The
unchanged implementation fails in 0.11 s with the native capacity refusal. This
is a reproducible mechanism matching the hosted error class, not proof that it
was the only cause of the hosted COMMENT failure.

Admission now permits at most 16 reclamation passes after definite native
capacity refusals, retaining the same workspace ID and no-retry command wrapper.
Each pass retires at most one manifest; it never scans or deletes row/index data.
When no retirement is confirmed, one final reservation can observe a slot freed
by a competing collector, then admission stops rather than polling indefinitely.
Any unknown reply, cancellation or different refusal terminates the loop before
SQL/input execution. Active owners, pins, compaction and the 16-generation limit
remain protected. Explicit sweep accounting is unchanged.

Five focused race cases pass (root 2.204 s), including the new competing-slot
case (three reservation attempts, one metadata clone, no row-GC claim, competitor
still live and root unchanged). A new lost-successful-reply-after-reclamation
case proves two attempts total, no metadata clone or SQL/input admission, and a
real server-side reservation. The existing initial unknown-reply, reclaimable
and all-active cases remain. Ten repeated runs of these five cases plus the
public concurrent COMMENT publication test pass all 60 test events, no skips or
failures, root 11.459 s. The fixture remains owned Software 8.8.0, strict TLS,
active/effective 4x QPF and noeviction.

Unit-only race verification after this change passes 1,037 test events,
936 expected integration/diagnostic skips, zero failures; root 53.320 s. The
versioned native build passes; SHA-256
`54de8fb71df05d8f4acd7d15bdd1e7d4803c2b86d0342cf11c661874ab128921`.
The complete snapshot race selection passes 372 test events, 24 explicit skips
and zero failures; root 284.131 s. Skips retain nine absent Cluster fixtures,
six reader-lease model cases, six permission/recovery cases and three opt-in
profiles. The versioned library passes all 113 selected native Arrow, layout,
statistics and partition tests, no skips, 26.50 s, after the Go database process
exited and the owned Software fixture was reset/reverified. Python 3.14.10,
PyArrow 26.0.1 and ADBC manager 1.13.0. No new sustained-throughput claim is made
for this admission-only change. The full ordinary c4698f3 run passes 1,945 events with 308 explicit
skips, root 1,168.550 s, before this admission change. The failed hosted 534cd6f
run is preserved in [its record](atomic-compaction-hosted-validation.md). Neither
result establishes current hosted/reader/platform parity.

Author review checks (1) bounded admission before execution/consumption, preserved
active leases and same-ID/no-replay behavior; (2) unchanged namespace, reference,
ACL/TLS and GC ownership boundaries; (3) the distinction between current focused/
snapshot/native checks, the preceding full ordinary source, failed hosted jobs
and unmerged upstream features. This is not maintainer approval. Hosted snapshot
coverage and required reader authorization remain incomplete.
