# Bounded inserts with durable batch outcomes

Date: 2026-11-03. Parent source fa4d2ad. Protocol remains 14, without migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Problem and contract

Pipelined writes still accumulated the statement's 100,000-row/64 MiB staging
counter. A real 100,001-row INSERT failed with the existing staging-budget error.
Arrow ingestion also converted an entire input record into Go row values before
writing. Raising the counters would not bound memory, recover uncertain outcomes
or reduce per-row Lua fence overhead.

SQL insert and Arrow ingestion now write at most 256 rows per atomic server
batch, targeting 1 MiB of arguments. One oversized row can exceed the target up
to the existing 64 MiB command ceiling. Every declared row key, exact int64 ID,
field layout, unused identity, directory type, writer lease and data owner is
validated before mutation. Required HSET/ZADD permissions are preflighted. Row
directory entries are registered before row images so unexpected runtime errors
cannot orphan images outside ownership-aware reclamation. Failed batches do not
publish, including when earlier batches succeeded.

One bounded outcome slot in the private work record records the last batch's
random operation ID and row count. Calls are serialized for that slot. An
uncertain reply triggers an independent, bounded read of the outcome. A matching
outcome permits progress; a missing/unknown outcome fails the statement. The
driver never resends the uncertain batch or reevaluates SQL/Arrow input.
Publication still requires complete input, constraints, index readiness and the
existing catalog CAS/rebase protocol. Reader visibility remains one generation.

Arrow conversion uses at most 4,096 rows per slice, releasing each slice after
its bounded writes. This limits additional converted row objects, not the size
of the caller's original retained record or an individual wide value. The original
input is consumed once. Late reader errors discard all private output.

The work-record outcome adds no separate key or ownership lifecycle. Older
protocol-14 clients ignore the optional field and cannot share this writer's
token; there is no new live-data representation or migration requirement.

## Verification

Owned Redis Software, strict TLS, Search, noeviction and active effective 4x QPF;
macOS ARM64. Database suites are sequential. The redis-up invocation stalled in
its proxy-certificate management HTTP update; that specific live request was
terminated. Effective QPF and noeviction were independently reverified through
the authenticated management API before corrected-code tests and benchmarks.
No configuration was inferred from a partial setup log. Operational artifacts
and credentials remain private.

- Before correction: 100,001-row INSERT failed with the cumulative staging error.
- Focused corrected SQL/Arrow race checks passed (14.980 seconds): 100,001-row SQL
  insert, one 100,001-row Arrow record, exact values above 2^53, a late Arrow
  reader error, single input consumption, checked Arrow allocator cleanup, lost
  batch reply resolution without resend and no visibility before publication.
- Complete snapshot race suite: 171 pass events, eight explicit skips (seven
  Cluster-only and the separately provisioned GC principal), zero failures,
  140.844 seconds. Final validation/preflight tests added afterward ran separately.
- Final malformed-batch race regression: all five cases passed, 2.041 seconds;
  bad last key/row ID, duplicate identity, oversized multi-row packet and an
  existing unregistered key leave the directory and earlier row untouched.
- Software API principal checks passed (1.856 seconds). HSET/DEL positive controls,
  denied ZADD/ZREM negative controls and each script's preflight error proved no
  partial row mutation. Temporary principal cleanup completed.
- Unit-only packages: 908 pass events, 753 expected integration skips, zero
  failures; this run preceded the final optional ACL/malformed test additions.
- Final wide-row race regression passed (2.656 seconds): one SQL INSERT stages
  33 two-MiB payloads (66 MiB total), publishes once and verifies all lengths and
  ID bounds. Single rows exceed the target while staying below the command limit.
- Versioned shared library rebuilt with make -C go build. Native C/ADBC snapshot
  validation: 13 passed, zero skips/failures, 13.425 seconds.
- Rebuilt IPC interface: nine passed, zero skips/failures, 1.09 seconds. TLS Ray
  datasource suite: 17 passed, zero skips/failures, 13.09 seconds, three upstream
  configuration deprecation warnings. These runs retain REDIS_URI and run
  sequentially after snapshot-native validation.
- Changed Go formatting, Python AST parsing, local evidence links and
  git diff --check passed.

## Performance comparison

Both revisions use versioned make builds, the same verified fixture and benchmark
script. SQL/Arrow batches contain 10,000 exact int64 rows. One warmup and three
measured repetitions per case; timings include execution through publication.
Every sample must then pass a complete ordered fetch against independently
calculated rows, outside the write timer. Samples and binary hashes are retained
in [write-batch-benchmark.json](write-batch-benchmark.json). Synthetic data only;
no ClickBench reload or run. Parent is fa4d2ad; current is that parent plus this
bounded-insert change, identified by the rebuilt binary hash. Three primaries
behind the proxy, one catalog hash slot; this is not a multi-slot throughput test.

| Case | Parent median ms | Current median ms | Change |
|---|---:|---:|---:|
| SQL insert, 10,000 rows | 461.499 | 339.858 | -26.4% |
| Arrow append, 10,000 rows | 466.536 | 331.418 | -29.0% |
| Metadata comment, 32 unrelated tables | 15.587 | 14.737 | -5.5% |
| Point update, 32 unrelated tables | 35.330 | 42.784 | +21.1% |
| One-row Arrow append, 32 unrelated tables | 25.989 | 25.073 | -3.5% |

Bulk publication improves in these samples. Small writes are mixed; the point
update median is slower and remains a performance concern, even though its
mutation path was not replaced. Three repetitions on one host are not a stable
all-workload performance baseline. Retain the slower sample rather than claiming
overall write parity or repeating until it disappears. The broader sustained
publication/compaction comparison remains required.

## Review and remaining scope

Reviewed server admission before mutation, namespace/owner fences, exact IDs,
bounded arguments/outcome state, runtime failure discoverability, cancellation,
uncertain replies, Arrow slice ownership and unchanged-table guards. Self-review
is not maintainer approval.

This is not process-crash resume of unread Arrow input or a persisted SQL plan.
Generic UPDATE/DDL rewrites still use the older cumulative staging limits. SQL
planning can materialize all rows, statements retain their 15-minute timeout,
and Redis must have noeviction capacity for private and retained generations.
An incomplete input operation is aborted/reclaimed, not resumed by replaying it.
Durable operation manifests/input spooling, generic rewrite checkpoints, sustained
compaction throughput, full platform/latest Cluster/recovery checks and broader
comparative performance remain open.
