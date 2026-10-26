# Verification and review

2026-10-22. Requestor alberttwong; implementation and self-review Codex.
Base `403e98f`. [Intent](intent.md), [contracts](spec.md), [plan](plan.md).
Human approval is pending PR review; no merge or release is included.

## SQL and Arrow

- Before implementation, all 11 new missing-feature SQL examples failed; retained
  [baseline output](evidence/sql-before.log). The explicit-range HASH-selection
  test also [failed on main](evidence/range-before.log) before changing the plan.
- [Focused integration checks](evidence/focused.log) cover DROP preflight (tables
  and views), persisted arrays and array views, all scalar storage round trips,
  Arrow list metadata, SELECT/FROM SRFs, lateral references, multi-argument
  UNNEST, ROWS FROM, ordinality, padding, empty/NULL results and invalid placement.
- [PostgreSQL comparison](evidence/pgdiff.json): 63/63 cases pass without new
  allowlist entries, including physical Arrow types, errors, DEFAULT/CHECK/window SRF placement,
  lateral LEFT JOIN and post-expansion LIMIT, common array types,
  non-UTC temporal arrays and post-error DROP preflight checks. Reference is the
  cached PostgreSQL 16.10 image, UTF8/C, rather than the harness's pinned image.
- Native ADBC validation: 380 passed, eight existing unsupported-capability skips,
  three subtests passed (cached Python 3.13 environment). The skipped features
  include transactions, statistics and a second catalog; see the complete log.
- Python 3.14 native tests: five passed, including exact Arrow list bulk ingestion
  and `WHERE id=ANY(?)` with a list parameter. NULL arrays, empty arrays, NULL
  elements, escaped strings and returned buffer ownership are checked.

## Exact range plan

The ordinary scan adds cap-bounded internal-ID windows for insertion order. Main
replaces its residual with the full WHERE even when the original Search terms
were exact. `wideHashFetch` and `newScanProjection` reject that redundant residual,
selecting LOAD-all instead of positional HMGET/Arrow output. Public partitions
already mark their discovery/fetch source authoritative.

Each bounded subrequest intersects the **unchanged original Search query** with
an exact internal-ID range. Retaining `wp.residual` is therefore sufficient; an
inexact comparison keeps its original recheck. The complete HASH fallback still
requires `sel.Where`. Tests that deliberately switch an index plan to HASH
pagination now explicitly restore that full predicate.

Observed selection decisions are retained in the focused log:

| Shape | Range source | Pushed limit | Order | Residual | Wide HASH |
|---|---|---|---|---|---|
| Exact explicit row-ID bounds | yes | none | row ID ascending | none | yes |
| Bound at 2^53 | yes | none | row ID ascending | preserved | no |
| Extra arithmetic predicate | yes | none | row ID ascending | preserved | no |
| LIMIT 2 OFFSET 1 | no | offset 1, count 2 | row ID ascending | none | no |
| Descending row ID | no | none | descending | none | no |

No eligibility guard was removed. Sparse/deleted rows, NULL/default fields,
BIGINT payload boundaries, bounds outside exact-double range, aliases/duplicates,
empty results, limits/offsets and exact order are compared with generic execution.
Existing tests cover post-discovery HASH updates/deletions, cancellation and
cursor cleanup. Discovery still uses actual Search keys. Pending cleanup,
readiness failures, adopted/external layouts, tie sorts and late projection keep
their existing fallback.

## Measured gate

Dedicated disposable Redis 8.6.2 on localhost; RESP3; batch rows 4096; independent
100K × 105 official ClickBench prefix, 79,578,019 logical Arrow bytes. Both regular
explicit SQL ranges and public partition descriptors use one/four persistent
readers. Each pair has one warmup and five alternating measured trials. Timing
includes complete fetch and Arrow IPC output; independent type/value/duplicate
validation occurs outside timing. Every one of 96 trials validated every selected
value and multiplicity. Actual first batches were 4096 rows for every reader.

| Projection/API | Readers | Main median seconds (range) | Candidate median seconds (range) |
|---|---:|---:|---:|
| Wide explicit ranges | 1 | 6.961 (6.732–7.015) | 3.276 (3.234–3.295) |
| Wide explicit ranges | 4 | 3.043 (2.878–3.078) | 1.527 (1.438–1.545) |
| Wide public partitions | 1 | 3.221 (3.220–3.254) | 3.212 (3.199–3.223) |
| Wide public partitions | 4 | 1.398 (1.368–1.491) | 1.393 (1.339–1.417) |
| Narrow explicit ranges | 1 | 3.085 (3.058–3.102) | 2.951 (2.879–2.978) |
| Narrow explicit ranges | 4 | 0.662 (0.631–0.721) | 0.594 (0.565–0.655) |
| Narrow public partitions | 1 | 2.862 (2.797–2.923) | 2.901 (2.868–2.927) |
| Narrow public partitions | 4 | 0.644 (0.566–0.729) | 0.513 (0.494–0.623) |

Wide explicit ranges changed from zero to 100,000 HMGETs. Median one-reader Redis
reply bytes fell from 340.27 MB to 153.17 MB, while request bytes increased from
0.013 MB to 192.90 MB (repeated field names). Four-reader replies were 341.26 MB
versus 154.15 MB. This is an execution/decoding improvement, not a reduction in
combined network traffic. Counters include small INFO/metadata traffic; they are
Redis server byte counters, not packet captures. The JSON retains every command
count, server CPU, client CPU, batch sizes and trial. One-reader client CPU fell
from 4.573s to 2.218s; four-reader CPU fell from 5.345s to 2.612s.

Separate Go diagnostics for three complete explicit-range scans allocated
16,734,773,320 bytes / 163,930,371 allocations on main versus 3,479,571,968 bytes /
99,111,944 allocations with the candidate. These are cumulative Go allocations,
not peak memory. CPU top tables are retained. Diagnostic timings are not the
promotion gate and overlapped separate integration testing. Reported Python RSS
is a process-lifetime high-water mark including earlier trials and validation;
it cannot establish per-query peak memory or a peak-memory improvement.

The measured candidate preceded final array typing/cancellation/parser edge
repairs. Its scan implementation is identical to the final branch. The
[manifest](evidence/manifest.json) records library/source hashes; raw results are
[equivalent range trials](evidence/wide-ranges.json),
[partition controls](evidence/wide-partitions.json),
[narrow ranges](evidence/narrow-ranges.json) and
[narrow partitions](evidence/narrow-partitions.json). The narrow one-reader public
partition median regressed about 1.4%; its elapsed ranges overlap. Do not present
that control as an improvement.

Reproduction uses `benchmarks/wide-transfer/compare.py` to load only the separate
fixture, then `cross_database.py --engines redis redis_candidate --modes persistent
--workers 1 4 --repeats 5 --single-reader-layout --redis-layout ranges` and again
with `partitions`; repeat both with `--columns watchid title eventtime`.
`TestSearchReplyScanProfile` accepts `REDIS_REPLY_PROFILE_SQL` for diagnostics.
No cloud deployments or costs were incurred.

## Three review passes

1. Correctness/lifecycle: retained exact residuals, global limits and internal-ID
   ordering; corrected array common types/time-zone coercion and escaped-space
   parsing; bounded/cancelled SRF expansion and regex matching. Added the final
   split-row resource-cap regression. Arrow input copies child values before
   input buffers are released; array slices/manipulations allocate output lists.
2. Ownership/security: multi-DROP preflights every distinct target before calling
   existing table/view deletion implementations. External HASH ownership,
   recovery, prefix and readiness checks remain in those implementations. There
   is no atomic-DDL promise for concurrent changes or transport failures. No SQL
   interpolation into Search was added by the fast-path change.
3. Intent/evidence: all four issue acceptance examples are covered. Arrays remain
   one-dimensional, unindexed and use one-based/default lower bounds. JSON SRFs
   remain #254. Bracket identifier quoting removal is documented as an upgrade
   change. Local OSS validation does not replace Software/Cluster/TLS CI.

The full suite and focused race logs are retained as [Go integration](evidence/go-suite.log)
and [race checks](evidence/race.log). Unit-only and differential-harness checks
are [unit suite](evidence/unit-suite.log) and [harness](evidence/pgdiff-unit.log).
Native validation ran on a separate disposable server, sequentially per server.
The host toolchain/performance binaries use Go 1.27.2; a separate Go 1.26.10 unit
pass, native build and 63/63 native differential cases verify the module's declared toolchain. This is not the
Go 1.26.10 release-build waiver documented in PR #454, and does not reuse it.

Initial failures included superseded SELECT-list GENERATE_SERIES rejection tests,
registry samples/argument checks, and the forced-HASH comparison's missing full
predicate. They were corrected without broadening skips. The first suite log is
retained. An extra unit-only attempt inside the filesystem sandbox could not bind
TLS/proxy test listener ports; it was rerun with local-network access.
