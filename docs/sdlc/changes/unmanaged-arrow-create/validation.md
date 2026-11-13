# Validation

Date: 2026-11-12. Implemented and locally validated on top of merged append
PR #400 (`cde3aaec`). Replacement remains unsupported and tracked by issue #399.
No release tag or storage protocol change is introduced.

## Contract and review

The new statement source option explicitly identifies an application prefix/index
and optional text/native encodings. Creation validates schema/configuration and
an empty prefix, creates a fresh membership-only index without replay, rechecks
prefix emptiness, and commits a create-only registration before consuming rows.
It then uses checked direct unmanaged insertion under a pinned registration.
The creation gate coordinates catalog metadata only; source rows have no
publication step. Existing create_append validates ownership/source/encodings
and retains its schema and index. Clearing the option restores managed creation.

An external schema refresh between registration and the row phase is refused
with 40001 before consuming input. This compares both identity and stored column
definitions: schema refresh retains a table's identity. Registration refuses to
refresh a competing definition even if it uses the same source. Error paths
preserve application indexes/registrations, partial row counts and SQLSTATE;
no automatic cleanup deletes application resources.

Review against `REVIEW.md` covered lifecycle, ownership, cancellation, uncertain
outcomes, schema/index handling, default behavior and documentation. No known
actionable issue remains; this is not external human approval.

## Checks and evidence

All live runs used the task-owned Redis Software RAM fixture originally created
with `make -C go redis-up`, verified before each run through its management API:
active 4x QPF, six Search workers, mnp routing, 32 connections, one shard and
noeviction. Authenticated certificate-verified TLS and RESP3 were used.

- Final focused race command, with `REDIS_SNAPSHOT_URI`:
  `go test -count=1 -p 1 -timeout=30m -race -json -run
  '^(TestUnmanaged|TestSnapshot.*Ingest|TestSnapshotBuildsOnlySealedDDLAndIngestIndexes)'
  .`: **86 pass events; zero skips, failures or race reports**.
- Final unit-only all-package command, with `REDIS_URI` and `REDIS_SNAPSHOT_URI`
  unset: `go test -count=1 -p 1 -json ./...`: **1325 pass events, 1230 conditional
  live/opt-in skips, zero failures**. No skip conditions were weakened.
- Focused new-feature tests with the Flex stopgap option and command guard on
  the same Software **RAM** fixture: `go test ... -json -run
  '^(TestUnmanagedArrow|TestUnmanagedSource)' .`: **61 pass events; zero skips or
  failures**. This is protocol coverage, not a physical Flex deployment or a
  performance result. The guard caught an initial-scan restriction; creation
  now uses SKIPINITIALSCAN plus the second empty-prefix proof. A race fixture
  inserts a row before FT.CREATE and verifies refusal without input or deletion.
- `make -C go build adbc-cli VERSION=v0.0.26-dev.unmanaged-create`: passed.
- Final Python/C shared-library smoke, ADBC manager 1.13.0 and PyArrow 26.0.1:
  creates a 1000-row/105-public-column collection, then create_append writes
  another 100 rows before an existing key returns SQLSTATE 23505 and acknowledged
  count 100 in the error detail. **All 1100 rows/types/values/multiplicities
  validated independently**. No managed row IDs were written; catalog root
  remained unchanged during the existing-table row phase. A test CA is supplied
  before Python process startup. No benchmark timing claim is made.
- Managed Arrow atomicity/modes/admission/index lifecycle and existing unmanaged
  source writes/ownership are included in the focused race run. The preceding
  append PR also completed the full ordinary Software suite (2270 pass events,
  548 conditional skips); this follow-up reruns the affected paths and all unit
  packages rather than claiming another full ordinary live run.
- Changed Go files are gofmt-clean; `git diff --check` and local documentation
  anchor checks pass. The existing benchmark coverage/arithmetic audit passes;
  historical v0.0.26 measurements are unchanged.

Cases include create/create_append, four concurrent creators, first-field NULL
membership, field encodings, duplicate names, existing prefixes including nested
non-row keys, existing indexes, managed-mode conversion, missing append targets,
views, retained/shared/staged restrictions, source identity/encoding mismatch,
option clear/default behavior, lost FT.CREATE replies, cancellation, competing
registration/schema refresh, bounded partial input and DROP/GC preservation.
Unknown-reply/registration-race injection targets the source-controller boundary
so the test delegate observes the actual non-replayable command; public API and
C-ABI paths are covered separately.

See [intent](intent.md), [spec](spec.md), [plan](plan.md) and the
[public contract](../../../unmanaged-tables.md#create-a-new-unmanaged-collection).
Operational identifiers, credentials, raw logs, data, licenses and slide payloads
remain private. Cleanup of the isolated fixture is recorded below.

Cleanup: the verified task-owned Software Compose project, its volumes and
network were removed after final validation. Private evidence is retained.
