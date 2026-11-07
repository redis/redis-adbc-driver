# Utility evidence

Date: 2026-11-06. Base: `9c838db7a24d40f35ac503efc7861a0805a8abde`.
Issue: [#341](https://github.com/redis/redis-adbc-driver/issues/341).
Status: local validation complete; delivery checks follow on the PR.

See the [entry-point audit](audit.md), [contract](spec.md) and
[numeric samples with candidate source hashes](memory-samples.json).

## Reproduced defects and correction

- A 17-page native import issued **19 descriptor SETs** before the correction.
  Repeated rows were compared with the original TAG/NaN facts, even after earlier
  batches had recorded the same facts. The final regression passes with **3
  descriptor writes**, including output bookkeeping, while checking exact
  normalized/NUL strings and NaNs. The correction reads only the current private
  metadata, checks the physical identity and omits already acknowledged facts.
  Published/shared paths, ownership fences and control admission remain intact.
- Keeping the original `CopyHashes` result alive kept a weak reference to its
  input scan alive after GC. The focused regression failed before the result
  correction and passes under the race detector afterward, preserving table/row
  and unknown/null-field counts. An independent statistics object avoids the
  interior pointer into the scan. No client-side copy acknowledgement cache or
  public API/storage-format change is introduced.

## Backend and checks

All backend runs used the task-owned default fixture, started/reverified through
`make -C go redis-up` and `software-setup.sh --exec`: **Redis Software 8.2.6-25,
Redis 8.8.0, strict TLS/RESP3, noeviction, active/effective 4x QPF**. The local
client toolchain inside `go/` is Go 1.27.3, darwin/arm64. Managed output remains
in one slot. Source reads are observed external data, not cross-shard snapshot
transactions. Operational logs, fixture identities, credentials and license stay
private; the checked-in samples contain only numeric observations/source hashes.

| Check | Result |
| --- | --- |
| `TestSnapshotUtilityCapabilityWorkIsBounded`, before correction | Expected failure: 17 pages / 19 descriptor writes |
| `TestHashCopyResultDoesNotRetainInput`, before result correction | Expected failure: returned statistics retain the scan |
| Focused result GC regression, `-race -count=1` | Pass; result remains live while scan becomes collectible |
| Full unit-only `go test -race -count=1 -p 1 -timeout=30m -json ./...`, backend URI variables unset | All six packages pass; 1,220 test/subtest passes and 1,089 skips. Backend skips are not compatibility evidence |
| Final Software utility/import/copy/ALTER race selection | 22 top-level tests pass; two existing journal-permission subtests skip because the separate HDEL/ZREM-denied principals are not configured |
| Additional public adoption/foreign/overlapping-prefix/index-policy race selection | Six tests pass, no skips; includes 100,001-row foreign import and 200,001-row HASH Apply |
| Final non-race `TestSnapshotUtilityCopyBoundaries` | All three cases pass, including complete source/result checks |
| `gofmt`; `git diff --check` | Pass |

The backend race selection was:

```sh
go test -race -count=1 -p 1 -timeout=15m \
  -run '^TestSnapshotUtility|^TestSnapshotImport|^TestSnapshotCopy|^TestSnapshot.*(Tag|NaN)|^TestSnapshot.*Alter' -v .
```

It runs through `software-setup.sh --exec` with both `REDIS_URI` and
`REDIS_SNAPSHOT_URI` set to that same verified disposable fixture. An earlier
selection unset `REDIS_URI` and failed an existing partition helper with an empty
URI; restoring the required test environment passes the unchanged assertion.
Initial invalid-UTF-8 VARCHAR fixtures were rejected by the existing Arrow input
validator, and a first binary assertion used unsupported SQL `encode`. The final
fixtures use valid UTF-8/NUL text and inspect the completely consumed public
BYTEA Arrow result directly. Neither validation policy nor feature support was
changed to make those fixture errors pass.

The extra public selection ran the following under `-race`:
`TestSnapshotExternalCopyCompatibility`,
`TestManagedHashImportIndependentIndexPolicy`,
`TestManagedForeignImportNativeIndexPolicy`,
`TestSnapshotForeignImportBeyondCaptureQuota`,
`TestSnapshotHashImportBeyondCandidateQuota`, and
`TestSnapshotForeignCaptureOverlappingPrefixes`.

## Boundary, failure and ownership evidence

- Public `CopyHashes` completes at **100,000 and 100,001 rows**; exact BIGINT
  `9007199254740993`, NULL, NUL/Unicode strings and separate arbitrary-byte BYTEA
  values are checked through completely consumed SQL/Arrow results.
- A **33 × 2 MiB = 66 MiB** payload copy completes in 33 single-row native
  batches. Narrow copies use at most 256 images per batch. Every acknowledged
  batch checks that the catalog root is still unpublished; an old pin cannot
  observe the final new table.
- Native row admission accepts exactly **64 MiB of KEYS + ARGV data**, rejects
  **one byte over**, preserves the earlier private row/directory and registers
  no rejected row. Explicit abort/reclamation removes the wide private row.
- Injecting a lost capability-metadata reply after server execution produces
  one mutation attempt, no replay/publication and no reread of captured input.
  Old pinned rows survive reclamation; failed/completed journals are removed.
- Canceling public replace after its first acknowledged private batch leaves the
  previous table/root intact. Public sweep reclaims the private output while an
  old pin retains its referenced prior rows.
- All seeded application HASH fields are checked after both complete copy and
  cancellation, including the source's ignored row-ID field. Additional public
  import/refresh tests retain application indexes/HASH encodings and separate
  native output index policy.

## Complete-operation memory observations

These are single **non-race** samples of the complete `CopyHashes` API call,
including publication/index readiness and cleanup. Input generation and later
query/source assertions are outside its elapsed time. They are not a controlled
latency comparison or a throughput guarantee.

| Source rows / payload per row | API ms | Native batches / maximum rows | Sampled heap growth (bytes) | Total allocations (bytes) | Heap delta after GC with result live (bytes) |
| --- | ---: | --- | ---: | ---: | ---: |
| 100,000 / 7 bytes | 7,124.405 | 400 / 256 | 34,843,088 | 516,823,272 | 345,880 |
| 100,001 / 7 bytes | 7,220.321 | 401 / 256 | 36,451,856 | 516,508,608 | 59,696 |
| 33 / 2,097,152 bytes | 459.732 | 33 / 1 | 346,068,920 | 548,623,520 | 17,616 |

Heap is sampled at acknowledged batch boundaries and API return; it is neither
exact peak heap nor RSS. Total allocations are cumulative, not retained memory.
Post-GC deltas include runtime/client caches and are not hard bounds or a leak
proof. The 66 MiB copy still retains its current Arrow record during conversion,
so its sampled live heap is much larger than the native batch target. Seen-key
deduplication remains operation-sized while copying; returned statistics no
longer extend its lifetime. Directory/sample-row `MEMORY USAGE` observations are
in the JSON and are not total database/server memory.

## Review and remaining limits

Reviewed against `REVIEW.md`: the private-only optimization reads current facts
without native index queries, checks identity and preserves every failed/unknown
outcome. Result detachment preserves the existing statistics API/maps. Boundary,
exactness, cancellation, source ownership and old-pin reclamation checks pass.
No actionable local finding remains; this is an agent review, not inferred human
approval. Final CI/merge results are recorded on the PR.

The [audit](audit.md) deliberately retains the cumulative **64 MiB control-work**
budget, including row-ID allocation, plus single-row, manifest/pin/segment and
deadline bounds. No public utility row-image route still uses the historical
100,000-HSET loop. Existing planning/sample/key-map/Arrow retention is documented
instead of promising unlimited input or constant process memory. Checkpoints do
not imply public process resume. The separate historical recovery limitation
and #345's approved unreproduced closure are not a causal fix or a change to the
snapshot correctness assessment.
