# Specification: narrow unmanaged aggregate reads

Date: 2026-11-18. Source: `b06aea4f`. Status: implementation under validation.
See [intent](intent.md) and [plan](plan.md).

## Read contract

For unmanaged tables with at least 64 public columns, an unsorted full scan
requiring at most half the columns may discover source keys through Search and
use the existing native HMGET reader. Filtered scans retain that reader too.
Broad projections and small schemas keep their established source. Ties and
late materialization retain their established plans. Required residual inputs
are included in the selected field set.

Each native projection observes one live HASH image. A non-NULL field establishes
existence. All-NULL vectors and direct native permission refusals use the existing
complete-image fallback; missing and non-HASH rows are omitted. Transport or
composed errors never authorize replay. No index-value numeric or string reducer
proof is extended to unmanaged tables. Metadata pins/readiness checks remain.
Direct scalar aggregate scans can discover up to 32,000 keys per page, reduced
by the fetched-field width toward the existing 200,000-value page target. Their
shared worker grant is bounded by page chunks (at most 32 workers). Grouped
inputs keep 4,096-key pages. Workers fetch disjoint 1,000-key chunks; decoding
and reduction follow page order. Large values and complete-image NULL/permission
fallbacks can exceed nominal page targets; this is not a process memory limit. Unknown outcomes cancel
and join sibling workers before return, without replay. Cursor cleanup precedes
release of the scan grant. The subsequent DISTINCT merge uses no fetch workers. Eligible scalar consumers
can decode exact native vectors in metadata order without building a HASH map;
complete-image fallback rows remain intact and are never combined with a
previous vector. Residual predicates keep their established map consumer.

Key-only scalar cursor discovery may decode an owned raw RESP3 frame through
its existing client and coordinator. Exact binary source keys are copied;
partial-result warnings and malformed replies fail without replay. Recoverable
cursor IDs are retained for bounded cleanup, including parser failures. Ordinary
and grouped cursor shapes retain their established decoder.

## DISTINCT contract

A lone COUNT(DISTINCT direct_column), without grouping, aggregate FILTER,
ordered aggregation, outer aggregate inputs or representative dependencies, can
stream exact SQL equality keys into tupleSpill. NULLs are omitted. Equal keys
across every spill run count once. BIGINT equality does not pass through doubles;
empty strings and embedded zero bytes remain exact. Other shapes retain their
existing reducers. Flex keeps its separate source and is excluded from this
DISTINCT planner path.

Buffers follow existing spill limits (8 MiB target or 65,536 entries), with at
most 32 inputs per merge pass. Oversized individual values may exceed the target.
Temporary files are removed on success, error and cooperative cancellation.
Killing the process bypasses cleanup and can leave its private spill directory;
a supervising process must reclaim abandoned files. Disk exhaustion
is an error; no partial count escapes and the query is not replayed.

## Acceptance and evidence limits

Run focused regressions on Redis Software with verified effective 4x QPF, TLS
and RESP3; verify exact values, NULLs, live changes, source ownership, failures
and reader lifetimes. Unit-only checks are not server compatibility evidence.
Full-scale issue closure additionally requires independently validated complete
Arrow outputs under the original resource/deadline contract. Keep original RAM
reproducer evidence separate from any Flex validation and preserve environment
provenance. A compatible full-scale backup is required for a restored rerun;
never substitute another distribution or silently reload the dataset.
