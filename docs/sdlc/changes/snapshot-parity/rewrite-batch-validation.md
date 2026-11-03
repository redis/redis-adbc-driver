# Bounded replacement, tombstone and DDL batches

Date: 2026-11-03. Parent source 40cc126. Protocol remains 14; no migration.
[Full parity](plan.md) remains incomplete.

## Reproduction and behavior

After bounded inserts, 100,001-row UPDATE, DELETE and ALTER still failed with the
cumulative staging-budget error. All three failures were reproduced on the
verified owned Redis Software fixture (race run, 66.614 seconds). Keeping an
actual reader pin protects the original physical source throughout each case.

UPDATE full-row replacements and DELETE tombstones now use the same bounded
row-image/outcome mechanism as inserts. ALTER/rekey copies converted images with
strict unused-row admission. Pages contain at most 256 rows with a 1 MiB target;
one wide row retains the 64 MiB command ceiling. Final index validation and catalog
publication remain separate and atomic. Compaction keeps its own durable job
and bounded copy protocol.

Replacement is limited to the writer's registered private generation. An existing
HASH must have its exact zero-score directory entry; an unregistered HASH/string
or a missing registered image is rejected. All identities, types and permissions
are validated before mutation. DEL permission is checked in addition to HSET and
ZADD. Registration precedes image mutation, and only private images can be cleared.
Complete replacements remove omitted/NULL fields instead of retaining stale
values. Full-row provenance survives updates and ALTER; tombstones remain private
visibility markers and keep the final overlay flag enabled.

Acknowledged batch writes are counted independently from the old unbatched
command quota. The initial integration check caught UPDATE/DELETE being treated
as no-ops when that accounting was absent; actual data assertions failed. No-op
detection now checks both counters, preserving empty-write optimizations while
publishing real batch mutations without reintroducing the cumulative limit.

Lost replies are resolved by reading the bounded work-record outcome. No SQL,
USING expression, row batch or Arrow input is resent or reevaluated. A failure
before execution leaves the committed root and logical values unchanged, even
when an earlier private page succeeded. No new persistent key, live row format
or migration is introduced; optional work-record outcome fields are unchanged.

## Verification

Owned strict-TLS Redis Software fixture, three primaries behind the proxy, Search,
noeviction and active effective 4x QPF verified with make -C go redis-up. Database
suites run sequentially on macOS ARM64. Infrastructure identifiers and credentials
remain private.

- Full snapshot race suite: 189 pass events, nine explicit skips, zero failures,
  209.127 seconds. Seven skips require the separate Cluster fixture; GC and
  insert permission principals are provisioned in the separate run below.
- All 100,001-row UPDATE/DELETE/ALTER cases passed with source pins, unchanged
  original HASH/directory checks, NULL field removal, exact large integers and
  converted strings. Existing no-op and private-baseline tombstone tests passed.
- Six before/after-execution interruption cases passed for UPDATE/DELETE/ALTER:
  failed admission preserves the committed root/values; lost replies complete
  with exactly three pages and 600 attempted images, without replay.
- Final Software permission probe (added after the full suite): all three GC,
  insert and replacement race cases passed, 1.985 seconds. HSET/ZADD positive
  controls plus a denied DEL prove the replacement-specific preflight before
  clearing a registered old image or registering another. Both temporary
  principals and their API resources were removed.
- Unit-only packages: 911 pass events, 769 expected integration skips, zero
  failures. This run preceded the final optional replacement principal test.
- Final registered-image admission regression: all five cases passed, 2.058
  seconds. A later unregistered HASH/string, missing registered image, nonzero
  directory score or same-UUID schema alias cannot partially change the earlier
  registered image, later object or directory.
- Versioned make builds completed for parent/current libraries. Rebuilt native
  C/ADBC snapshot suite: 13 passed, zero skips/failures, 12.611 seconds. IPC:
  nine passed, 1.09 seconds. TLS Ray datasource suite: 17 passed, 13.11 seconds,
  three upstream configuration deprecation warnings, zero interface skips.
- Changed Go formatting, Python AST parsing, local evidence links, JSON parsing
  and git diff --check passed.

## Comparative samples

Parent 40cc126 and this row-image change were built independently with the same
versioned make target. Binary hashes and complete samples are preserved in
[rewrite-batch-benchmark.json](rewrite-batch-benchmark.json). Same verified fixture,
10,000 exact int64 input rows, one warmup and three measured repetitions per case.
Each timing includes mutation through publication. Complete independent ordered
result validation follows every sample, outside the timer; DELETE must fetch an
empty result and ALTER must return all exact converted strings. Setup is excluded.
No SQL retry loop, ClickBench run or reload is involved.

| Case | Parent median ms | Current median ms | Change |
|---|---:|---:|---:|
| UPDATE all rows | 3010.568 | 2669.141 | -11.3% |
| DELETE all rows | 776.815 | 642.215 | -17.3% |
| ALTER BIGINT to VARCHAR | 1405.767 | 1219.685 | -13.2% |

These short synthetic samples show improvement for these batch workloads. They
do not settle sustained compaction, arbitrary schemas, small-write variability
or cross-platform performance. The prior slower point-update sample remains
historical evidence and has not been erased or declared resolved by this run.

## Review and remaining work

Reviewed shared-source immutability, private registry membership, directory-first
failure discoverability, NULL/provenance preservation, exact IDs, permission
preflight, no-op/tombstone accounting, outcome resolution and final publication.
Self-review is not maintainer approval.

Bounded batching does not persist an entire SQL plan or unread input. Process/input
resume, remaining import/utility capture quotas, metadata admission, statement time
limits, SQL result/change materialization, sustained compaction throughput and the
full platform/latest Cluster/failover/persistence matrix remain open. No complete
feature or performance-parity claim follows from these checks.
