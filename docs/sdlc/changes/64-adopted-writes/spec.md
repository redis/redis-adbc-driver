# Specification: adopted writes

Date: 2026-10-30. Source: `95d6583`. Author: Codex. Status: implemented.
[Intent](intent.md), [plan](plan.md), [validation](validation.md),
[issue #64](https://github.com/redis/redis-adbc-driver/issues/64).

## Functional requirements and implementation

- Adopted KeyRows tables accept an explicit string `__key` input. It is an
  ephemeral input column, excluded from implicit INSERT targets and RETURNING
  stars, never persisted to metadata or HASH fields. RETURNING can name it.
- Require a non-NULL key accepted by `isRowKey`. Reject duplicate batch keys,
  all-NULL payloads, NOT NULL violations, or existing keys of any Redis type.
- Validate batches before writes; atomically EXISTS/HSET each row with a
  single-key Lua script. Store only non-NULL column fields, with column field
  mappings and sampled application text formats. Do not allocate sequence IDs.
- SQL VALUES, SELECT, MERGE INSERT, and Arrow append/create_append share the
  keyed writer. MERGE preflights inserts before applying updates/deletes.
  Replace ingestion with __key fails before dropping metadata.
- Statement option `adbc.redis.truncate_adopted` accepts only `true`/`false`,
  defaults false, and does not propagate to another statement or connection.
- Opted-in TRUNCATE scans every primary with an escaped literal prefix,
  deleting only row HASHes with type-check/UNLINK scripts. Preserve nested keys,
  non-HASH values, metadata and indexes. RESTART IDENTITY does not allocate or
  reset application keys. Reject mixed managed/adopted lists before deletion.
- Re-keying RENAME and ADD COLUMN remain refused, DROP preserves HASHes.

## Data and execution design

`adopt_write.go` holds the input metadata adapter, validation, atomic row
creation and in-place truncate. Existing managed allocation/truncate paths
remain in `store.go`. SQL insertion, parameter description, RETURNING and Arrow
mapping consume the adapter only when inserting into adopted KeyRows tables.

## Risks and unresolved concerns

Creation is atomic per key, not across a statement or Arrow stream. A concurrent
application collision or connection failure can leave earlier inserts committed.
TRUNCATE is a bounded scan, not an atomic emptying; pause application writers.
Cancellation or failure can leave earlier deletions committed, including in a
multi-table statement. Redis metadata fencing cannot coordinate application
writers. These limits are documented in the driver reference; rollback must never delete another
writer's application key. Existing system-column restrictions in joins remain.

## Acceptance criteria

Verify actual HASH fields and text encoding, explicit and missing keys,
duplicate/non-HASH/nested/out-of-prefix collisions, competing writers, prepared
parameters, SELECT/RETURNING, MERGE, Arrow append/create_append, default refusal,
opt-in truncate isolation, mixed-table rejection, and preserved unrelated keys.
Run unit-only and Redis Software integration suites and build the native library.
