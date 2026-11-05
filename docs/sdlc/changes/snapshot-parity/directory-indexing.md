# Directory-driven final indexing

Date: 2026-11-04. Parent: e670a71. Status: implemented; final scoped validation passed, comparative performance pending.
PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[readiness attribution](point-write-profile.md) and
[the failed sustained-write gate](sustained-write-validation.md).

## Contract and implementation

Ordinary SQL/Arrow outputs now create one empty final Search index after row
execution is complete. Instead of a database-wide initial scan, the driver
traverses the registered private row directory in lexicographic pages. Each
notification batch contains at most 256 rows and targets 1 MiB of key/argument
bytes, retaining the existing 64 MiB single-row ceiling. A long-key fixture checks
that the byte target splits a page even when fewer than 256 rows are involved.
The physical prefix comes from its authoritative data-owner record, not a
second caller-supplied prefix or a UUID alias.

Before notifying any row, one Lua invocation validates the whole batch:
workspace state/token/expiry, live data ownership, exact physical namespace,
canonical positive int64 directory IDs, zero membership scores, HASH types,
matching stored row IDs, unique members and HSET permissions. It then HSETs the
unchanged row-ID field. Every HASH image, NULL/provenance field and directory
entry remains unchanged. A missing HASH cannot be recreated as a partial image.
IDs never pass through Lua doubles. Only staging outputs are admitted; background
compaction retains its separate resumable copy/index/install implementation.

The new index has no prior document versions: every notification supplies an
already-final complete image to its first insertion. Redis documents the
[SKIPINITIALSCAN option](https://redis.io/docs/latest/commands/ft.create/) and
[background search visibility on Redis 8.8 with workers enabled](https://redis.io/docs/latest/develop/ai/redisvl/0.27.2/concepts/search-and-indexing/#search-visibility-after-writes).
The driver therefore does not equate HSET completion or a document count alone
with publishability. It retains indexing/progress/failure/prefix/cardinality
checks and additionally requires a warning-free query over indexed positive row
IDs to match the complete physical directory for a nonempty output. Query errors
or partial-result warnings cannot publish a partial index. Empty output still
requires the corresponding zero-count metadata/prefix proof.

Notification commands retain the no-retry wrapper. An uncertain reply fails
publication and retires private output; it does not resend notifications, SQL or
Arrow input. Cancellation between batches stops population, leaving the public
root and old readers unchanged. The algorithm verifies traversed and final
directory cardinalities and retains the existing final publication/CAS fences.

A write-only principal need not acquire a new query privilege. Only a typed
Redis NOPERM refusal from the indexed-row proof selects a new private index with
the original initial-scan completion proof. The prior private index is retired.
A transport error containing NOPERM does not select this fallback. Classification
occurs before general ADBC wrapping discards the Redis error type. This changes
private index construction only; it does not replay row writes or input. The
fallback is slower, and its role-specific behavior is covered by an injected
server-refusal model rather than a newly provisioned Software principal.

No protocol bump, migration, disable switch, native grants or deployment-policy
change is included. Storage remains format 15. Managed external copies/refresh,
autocommit-only scope, Active-Active exclusion and noeviction remain required.

## Verification and limitations

Before the implementation, a real one-row output launched one database-wide
initial scan; the focused regression failed on that observation. It passes with
the new path while checking target values, full counts/sums and unrelated data.
The first setup invocation never reached Go tests because an already-satisfied
certificate PUT stalled; [the separate fixture repair](fixture-request-validation.md)
records that incident and its six passing mock tests.

Focused tests cover 513-row batches, exact BIGINT values beyond 2^53, NULL/Unicode
payloads, tombstones, long physical keys, missing/wrong-type/wrong-ID HASHes,
nonzero scores, duplicates, prefix aliases, aborted/expired/stale-token owners,
unknown successful replies, cancellation status, partial index-proof warnings,
and typed permission fallback versus transport failures. The rejection tests
compare index counts before/after the deliberately damaged fixture: the wrong-ID
HSET itself can legitimately create an index entry before the tested batch.
This correction retains the requirement that a rejected batch notify no earlier
row. No actual restricted-principal ACL-denial run is inferred from the model.

An intermediate complete snapshot selection passed 349 events with 23 explicit
skips; root 275.138 s. After adding the compatibility fallback, 27 selected
fault/readiness/empty-query/row-value/rekey events passed; root 10.735 s. Its first
attempt exposed that ADBC wrapping dropped the raw server error type; the final
proof helper classifies the raw error first. Final unit verification passes
1,035 events with 918 expected integration/diagnostic skips, root 51.701 s.
An earlier native build passed all 113 selected Arrow/extension/partition/
statistics tests (24.78 s); final snapshot/native validation after the last
argument-layout refinement is recorded below when complete.

All database runs use the owned Software fixture, strict TLS, active/effective
4x QPF, noeviction, Redis 8.8.0 and three primary shards behind its proxy.
Go 1.27.3/macOS ARM64; native validation uses Python 3.14.10, PyArrow 26.0.1 and
ADBC manager 1.13.0. The measured old 32c9d2f native binary has been preserved
and hash-verified for fresh comparisons. There is no new production throughput
or all-workload parity claim until complete fetch/value-checked comparisons run.

Author self-review checks whole-batch preflight, exact-prefix ownership, immutable
image handling, non-replayable mutations, warning/error classification and
unchanged publication/root references. This is not maintainer approval. Current
Cluster API/HA/persistence validation, actual restricted-reader authorization,
public process/input resume, sustained compaction improvements and complete
feature/performance parity remain open.

## Final checkpoint

The complete final snapshot selection passes **353 events, 23 explicit skips,
zero failures**, root 275.554 s. Skips are nine Cluster fixture cases, six
isolated-reader model cases, six permission/recovery cases and two opt-in
profiles. They remain open evidence gaps, not inferred successes. Final unit-only
race verification passes **1,035 events, 918 expected skips, zero failures**,
root 51.701 s. The versioned native build passes, and its final four-file native
selection passes **113 tests, no skips**, 25.29 s, after all database Go processes
terminated and the owned fixture was reset/reverified.

The final native library SHA-256 is
`a36f1eddbf40dcda8f5e6a083f8540232201e49f3aa525c4d1b30eef8e97bce7`.
The final full snapshot selection and native build/tests use the same production
Go source. `git diff --check` passes. No hosted/platform-wide result or current
full ordinary-suite result is inferred from these selections.

Each output image now incurs an additional unchanged row-ID notification, and
nonempty output gets an indexed-row count proof. Large-output and AOF costs still
need comparative measurement. The old 32c9d2f sustained benchmark remains a failed
parity gate; neither prototype timing nor regression elapsed time replaces a
fresh validated native comparison. Background compaction still uses its existing
initial-scan indexing phase and needs separate performance/recovery work.
