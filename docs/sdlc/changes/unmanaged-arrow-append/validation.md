# Validation

Date: 2026-11-12. Implemented and locally validated. Production base is v0.0.26
(`5c9d583a`); branch includes the subsequently merged benchmark evidence #398.
No release tag or storage protocol change is introduced.

## Reproduction and implementation

The new `TestUnmanagedArrowAppend` failed against the exact v0.0.26 checkout for
both HASH adoption and foreign-index registration: Arrow ingestion returned
NotImplemented. With this change both pass. The append path pins registration
metadata, bypasses managed workspace/publication/admission, and reuses checked,
non-replayable SQL source insertion. Conversion is bounded by 4096-row slices.

Failed admitted writes return acknowledged-row counts and the UTF-8 error detail
`adbc.redis.ingest.acknowledged_rows`, preserving status/SQLSTATE. Counts are
lower bounds when a reply is lost. No new transport/input retry is introduced.
The source scripts retain existing-key refusal and per-row atomicity.

## Checks

All live checks used a dedicated Redis Software RAM fixture created with
`make -C go redis-up`. Active 4x QPF, six Search workers, mnp routing,
32 connections, one shard and noeviction were verified through the management
API before each run. Connections used authenticated, certificate-verified TLS
and RESP3. No Community, Valkey or managed-cloud substitute was used.

- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -json
  ./...`: **1321 pass events, 1205 conditional skips, zero failures** across all
  packages. Skipped live tests are covered separately where relevant.
- Full ordinary Software suite, `go test -count=1 -p 1 -timeout=30m -json ./...`
  with the verified fixture's `REDIS_URI` and retained-control policy declaration:
  **2270 pass events, 548 conditional skips, zero failures**. Of those skips,
  416 require the separately enabled snapshot fixture, 17 use protocol ACL
  commands unavailable at the Software proxy, and 115 are other opt-in
  profiles/fixtures or inherited conditional cases. No skip condition was weakened.
- Focused Software snapshot/unmanaged race run:
  `go test -count=1 -p 1 -timeout=30m -race -json -run
  '^(TestUnmanaged|TestSnapshot.*Ingest|TestSnapshotBuildsOnlySealedDDLAndIngestIndexes)'
  .`, with `REDIS_SNAPSHOT_URI`: **59 pass events, zero skips/failures/race reports**.
- Flex stopgap option enabled on the same Software **RAM** fixture, with the
  existing snapshot Flex command guard: `go test ... -json -run
  '^TestUnmanagedArrow' .`: **34 pass events, zero skips/failures**. This is
  protocol-path coverage, not a physical Flex deployment or performance result.
- `make -C go build adbc-cli VERSION=v0.0.26-dev.unmanaged-arrow`: passed.
- Python/C shared-library smoke, PyArrow 26.0.1 and ADBC manager 1.13.0:
  **1100 rows x 105 public columns independently validated**, including all
  values, physical types and multiplicities. Initial 1000-row append succeeded;
  a later stream wrote 100 rows then refused an existing key with SQLSTATE
  `23505`. Python received the acknowledged-row detail as `(bytes, bytes)`.
  Managed catalog root remained unchanged; no managed row-ID fields were written.
  The launcher supplies the test CA before process startup. This is a correctness
  check, not a benchmark.
- Changed Go files are gofmt-clean; `git diff --check` passes. Local documentation
  links/anchors checked. The benchmark coverage/arithmetic audit still passes.

The new tests cover adopted/foreign targets, explicit/binary/dictionary string
keys, duplicate/out-of-prefix/NULL keys, schema/conversion errors, stream-schema
changes, bounded partial progress, lost replies with no replay, cancellation,
concurrent writers, field renames/native/text encodings, DROP/GC ownership,
unsupported modes, shared snapshots and retained/staged-write refusals before
input consumption. Managed Arrow modes, atomicity and admission regressions remain
covered. Review against `REVIEW.md` found no known actionable issue; this is not
an external human approval.

The task-owned fixture was reused for the explicitly requested unmanaged
`create`/`create_append` follow-up and removed after its final validation. Operational identifiers, credentials, raw
logs, datasets, licenses and slide-edit payloads remain private. Benchmark/deck
results remain labeled v0.0.26; this append feature is an unreleased change.

See [intent](intent.md), [spec](spec.md), [plan](plan.md) and the
[public API contract](../../../unmanaged-tables.md#write-arrow-data-to-source-hashes).
