# Verification and review

Date: 2026-11-20. Base: `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally verified; awaiting human review; no release recorded.

[Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md)

Local verification completed at combined integration source `4a6a40f7`.
The PR depends on [#561](https://github.com/redis/redis-adbc-driver/pull/561)
and targets its `codex/live-partitions-550` branch until that dependency merges.
Dedicated Software RAM and Flex fixtures used active
4x QPF, six effective Search workers, mnp routing and 32 connections, verified by
the repository setup paths. RAM uses one primary shard under local resource
constraints. Infrastructure names, endpoints, credentials, trust files and raw
logs remain private/ignored.

The first fixed-source fixture incorrectly assumed its filtered application index
would include a NUL-bearing key. Native singleton membership returned zero on
Software RAM; the earlier [Flex unmanaged record](../flex-unmanaged/validation.md)
documents the same limitation. The retained fixture independently checks native
membership for that actual NUL key, requires exact partition agreement, and
preserves its raw HASH/TTL. Non-UTF8 indexed keys must be returned with exact
bytes and values. No generic skip hides the native omission or claims full
NUL-index inclusion support.

The initial Flex preservation test assigned source TTL after calculating its
membership oracle, changing that source's native membership. TTL setup now
precedes the membership oracle, preserving a fixed source during partition reads.

Completed at the measured source before the later cached-layout regression:

- Focused race selection passed on Software RAM and actual Flex. Cases include
  1/4/11-worker exact fixed-source coverage, overlapping prefixes, non-UTF8 keys,
  native NUL membership, exact BIGINT/NULL values, source DUMP/PEXPIRETIME
  preservation, independent processes, source mutation/deletion/expiry, pinned
  registration drop, changed index, server admission expiry and forged descriptor
  expiry, typed parameters and native permission/transport/warning/malformed replies.
- Scoped HASH-reader unit cases accept only direct native WRONGTYPE with an
  accounted pipeline refusal. Wrapped/joined command/outer errors and unaccounted
  native refusals fail without replay.
- Existing managed partition and unmanaged projection/fault regressions passed
  in the selected RAM race run. Unit-only all-package checks and `make build`
  passed; Redis integration cases skip in the unit-only pass.
- [Synthetic comparison](../../../../benchmarks/unmanaged-partitions/report.md)
  completed 18 validated trials across both products at source `222ab181`.
  Four buckets beat one bucket here; ordinary RAM SELECT was substantially faster.
  The measurements do not establish a general speedup or full-scale behavior.

A later new test reproduced cached explicit-layout slices escaping after actual
child-pin loss. The shared fix from #550 (617061b7) validates additional cached
slices without adding another first-slice proof. The combined feature tree now
passes the retained #549 regression. This later coverage is not relabeled as
coverage of the earlier measured source.

Combined integration merges the published #550 branch into this branch, preserving
measured 222ab181 ancestry. There is one shared partition source option/getter,
union managed/unmanaged/live_hash values, generic deferred-only retained refusal,
and disjoint version-5/version-6 read dispatch. Partition-only unmanaged callbacks
now check complete native index identity before every exposed canonical batch and
cached layout slice. Canonical and cached-slice index-change regressions pass.
Ordinary unmanaged scan behavior is unchanged.

Final local combined checks on the source tree described above:

- Software RAM: `REDIS_SNAPSHOT_URI=... REDIS_URI=... go test -race -count=1 -p 1
  -v -timeout=8m -run '^(TestLiveHashPartition|TestUnmanagedPartition|TestLayout.*Buffered|TestSnapshotPartition)' .`
  passed in 17.420 s, including both external ownership modes, independent processes,
  shared cached layout lifetime checks and existing managed partition cases.
- Actual Software Flex: `REDIS_SNAPSHOT_URI=... REDIS_SNAPSHOT_FLEX=true go test
  -race -count=1 -p 1 -v -timeout=8m -run '^TestUnmanagedPartition' .` passed in 8.472 s.
  Live HASH registrations remain unsupported on Flex; this does not imply otherwise.
- Source descriptor credit refusal is checked before duplicating large query text,
  retains SQLSTATE 53200, releases its group/catalog pins and preserves sources.

The separate benchmark reports actual old source 222ab181 and file fingerprints;
none of the final integration/cached-layout/descriptor-credit changes are
retroactively claimed as performance or coverage of that earlier source.

Skips are the standalone subprocess helper and opt-in profile without its flag;
there is no generic functional skip for NUL membership. Final all-package unit
checks (`go test -count=1 -p 1 ./...`, with Redis endpoint variables unset)
passed; the root package took 28.604 s. `make build` passed using its normal
version flag. `git diff --check`, local Markdown link targets and the frozen
measurement source fingerprints passed. Both dedicated fixture containers and
their network were retired after testing; generated endpoint environments were
removed. Private raw logs remain outside the committed tree.

The native filtered-index NUL membership investigation is tracked separately in
[#559](https://github.com/redis/redis-adbc-driver/issues/559); source values are
not inferred to be index members from prefix/FILTER-looking fields.

Additional post-measurement regression: explicit unmanaged adoption reads its
single collection prefix, excludes subsequently created nested collections,
preserves source fields, and returns valid empty buckets. The dedicated RAM
`-race` case passed. This does not relabel the measured fixture.

## Review boundary

Self-review covered all three REVIEW.md passes: full predicate retention and exact
HASH values; byte-key hashing/deduplication and owned state/work admission; direct
native refusal/pipeline proofs; registration-only catalog pins; index identity
checks including cached output; independent process permissions and cleanup; and
source HASH/index/TTL ownership. No source mutation or row-ID field insertion is
introduced. No remaining actionable finding is identified in the reviewed scope;
this is not human review or merge approval.

Validation covers one-shard Software RAM and compatible actual Software Flex.
Cluster API, resharding/failover, Cloud, every release wheel and sustained/full-scale
workloads are not established by these local runs. SCAN/native HASH replies and
Arrow values retain their ordinary allocation behavior; state/work credits are
not a hard RSS ceiling. Repeated scans and source/index live changes remain
explicit design costs, not managed snapshot guarantees.
