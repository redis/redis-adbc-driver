# Implementation and evidence

Date: 2026-11-12. Base: a8de1c3c. Status: implemented and validated locally.
See [intent](intent.md) and [specification](spec.md).

1. Add explicit mode selection and external descriptors with zero data edges.
2. Register source identities without copying rows; preserve managed defaults.
3. Execute source DML without managed staging or mutation replay. Fence other
   mutation paths and snapshot-only APIs; retain native source ACL enforcement.
4. Exercise source ownership, live reads/writes and managed regressions.
5. Document options, consistency, lifecycle and validation evidence; review
   correctness and source ownership using REVIEW.md.

## Verification

- `make -C go redis-up` and the fixture's `software-setup.sh --exec` verified
  Redis Software 8.8.0 with active scaling factor 4, six Search workers,
  `mnp` routing, 32 connections and noeviction before integration runs.
  Snapshot tests used the owned disposable fixture, separately from ordinary
  suites. Endpoint details and generated operational files remain private.
- From `go/`: `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  passed. Integration tests skipped in this unit-only pass, as expected.
- `TestUnmanaged*` passed with `-count=1 -p 1` on that verified fixture.
  Coverage includes live reads, source INSERT/UPDATE/DELETE for adoption and
  foreign discovery, binary keys, missing/invalid keys, source field mappings,
  text boolean encodings, TTLs, filtered multi-prefix membership, source survival
  after DROP/sweep, missing-index unregister, snapshot/preparation/TRUNCATE
  refusals, managed-copy isolation, mode conflicts and uncertain-write refusal
  without replay. Dry-run checks verify mode/encoding instructions and no
  publication.
- The focused managed regression run passed using this selector with
  `-count=1 -p 1 -timeout 10m`:

  ```text
  ^(TestUnmanaged|TestSnapshot(ExternalCopyCompatibility|ImportStartsBesideExternalData|AlterCompatibility|SharesUnchangedTablesAndReclaimsReferences|Metadata|Sweep|IndependentPublication)|TestManaged(HashImportReport|ForeignImportNativeIndexPolicy)|TestSharedSnapshot(MetadataAndSequentialReads|RejectsUnsupportedUse)|TestNativeReaderHashVisibility)
  ```

  It exercises shared metadata/reference ownership, independent publication,
  sweep, managed imports, shared snapshots and native managed reads alongside
  the new mode. It is not a full Software/Cluster/ACL acceptance run.
- `make -C go build` passed with the repository's shared-library version flag.
- `git diff --check`, local Markdown target checks and Python syntax checks
  passed. No performance benchmark or unsupported-server test was run.

## Review

Self-review followed REVIEW.md's correctness/lifecycle, source ownership and
contract/evidence passes. Source mutations use one-shot commands outside the
managed metadata-slot fence, with key/prefix checks and a live catalog pin.
Unmanaged descriptors carry no source-data reference edges. DROP preserves
external indexes/HASHes even after source-index removal; legacy TRUNCATE opt-in
cannot bypass unmanaged refusal. Dry-run reports retain unmanaged semantics.

Limitations are explicit: an existing application index is required, filtered
foreign INSERT is refused, source writes can partially succeed, and snapshot-only
APIs/Arrow ingestion do not support unmanaged targets. Remaining validation
scope includes the full Software/Cluster/ACL matrix and packaged-language
end-to-end acceptance. No release, merge or human approval is implied.
