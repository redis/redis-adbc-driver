# Atomic import output and capture consumption

Date: 2026-11-03. Parent source f275963. Storage protocol remains 14;
no migration. [Full parity](plan.md) remains incomplete.

## Reproduction and behavior

The previous journal implementation committed output images and then separately
deleted captured payloads and directory entries. An observer immediately after
the row batch still found its captured input. Both normal-reply and lost-reply
regression cases failed before this change (1.034 seconds). The existing import
aborted/reclaimed on errors; it did not expose a resume API. The gap mattered for
future recovery because a new owner could not safely interpret unconsumed input
as unfinished output.

Import row batches now declare the two private journal keys and metadata presence
directory alongside the existing output keys. They validate exact workspace names,
registered private journal references/types/presence, unique original source keys,
captured payload membership and score-zero directory entries. HDEL/ZREM and
metadata presence permissions are checked before any mutation, in addition to
the existing row/directory/control preflights.

One script creates registered output images, removes precisely their captured
payloads and source-key directory members, updates metadata presence bookkeeping
and writes the durable row-batch outcome. A lost reply resolves that outcome
without replaying input or output. Wide capture pages can split into several
output batches; each consumes only its own source keys. All keys share the
catalog hash slot; original application keys are field values, not command keys.

Before mutation, import batches set an explicit incomplete marker and clear the
prior row outcome. Successful completion replaces it with the acknowledged
outcome. An unexpected Lua runtime failure therefore leaves an abort-only state,
rather than an apparent clean continuation boundary. Further row batches,
sealing and publication reject it. The final catalog publication still follows
complete capture, output, constraints, index validation and journal pruning.

Capture HSET/ZADD remain separately fenced: an uncertain capture mutation aborts
without replay and is reclaimed. No durable job retention, input-complete plan,
exclusive process takeover or public resume handle is introduced. The
[remaining resume design](import-resume-design.md) records those requirements.

## Focused verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries behind the proxy,
Search, noeviction and active effective 4x QPF verified using make -C go redis-up.
Database-mutating checks run sequentially on macOS ARM64. Operational artifacts,
credentials and infrastructure identifiers remain private.

- Atomic output/lost-reply, capture interruption/cleanup, overlapping prefixes,
  codec and existing source-key behavior: 14 pass events, zero skips/failures.
- Invalid captured-input admission: six cases plus parent passed, 2.105 seconds.
  Missing/duplicate input, nonzero score, wrong namespace, missing registration
  and an incomplete marker leave earlier output and all captured input unchanged.
  The incomplete-marker case also checks sealing/publication rejection.
- Software API permission probe: all GC, insert, replacement and two independent
  import-consumption denials passed, 1.979 seconds. Positive HSET/ZADD and the
  other consumption command distinguish the denied HDEL or ZREM. Work records,
  output images/directories and both captured inputs remain unchanged. All four
  temporary principals and their API resources were removed.
- Unit-only packages: 918 pass events, 796 expected integration skips, zero
  failures. This run preceded the final wide-page regression.
- Full snapshot race suite: 219 pass events, 14 explicit skips, zero failures,
  296.223 seconds. The separate Cluster fixture and temporary permission
  principals account for the skips; permission checks ran separately above.
  Foreign 100,001 rows passed in 50.10 seconds; HASH adoption 200,001 rows in
  33.20 seconds. The wide-page split case passed with exactly two single-row
  output attempts and one source-input call, including a lost second reply.
- Rebuilt native C/ADBC snapshots: 13 passed, 12.122 seconds. IPC: nine passed,
  1.07 seconds. TLS Ray: 17 passed, 12.96 seconds, three upstream deprecation
  warnings, zero interface skips. Ordinary managed import/adoption/foreign race
  selection: 31 pass events, three ACL skips, zero failures, 12.488 seconds.
  Changed Go formatting, Python syntax, local evidence links, benchmark JSON
  and git diff --check passed.

## Comparative refresh sample

Parent/current native libraries were built independently with the versioned
make target. [Complete samples and binary hashes](import-output-benchmark.json)
record the same verified fixture, 10,000 application HASHes, one warmup and three
measured repetitions. Timing includes explicit refresh through atomic publication;
setup and complete independent source/copy checks are outside the timer. Inputs
change before each refresh, so an old managed copy cannot pass validation.

| Case | f275963 median ms | Atomic output median ms | Change |
|---|---:|---:|---:|
| Explicit foreign refresh | 4492.822 | 4122.756 | -8.2% |
| Metadata comment, one-row table / 32 unrelated tables | 15.574 | 13.902 | -10.7% |
| Point UPDATE, same small-write fixture | 40.585 | 30.190 | -25.6% |
| One-row Arrow append, same small-write fixture | 34.494 | 37.440 | +8.5% |

This short synthetic sample shows improvement against the published journal
implementation. It does not establish general import or pre-snapshot performance
parity. The preceding journal checkpoint's measured +4.9% regression remains
historical evidence; a different run's absolute median is not a controlled
comparison against that earlier baseline.
Small writes use a separate reset fixture, one warmup and three samples per case,
with complete independent result validation after every write. Their medians
move in both directions, including the metadata comment that does not use the
changed row-image path. These short samples do not establish causality or settle
small-write variability. The Arrow append regression and prior slower point-update
sample remain open performance evidence; no repeat-until-green run replaces them.

## Review and remaining work

Reviewed exact registered namespaces, source-key identity, metadata presence,
directory-first discovery, permission admission, single-attempt execution,
unknown-reply resolution and fail-closed incomplete outcomes. Self-review is
not maintainer approval. The incomplete-marker test injects durable state; it
does not claim to reproduce every possible Redis runtime failure or partial
persistence behavior.

The new checkpoint does not close process resume, adoption planning memory,
exceptionally wide source-page allocations, broad-source ownership edges,
sustained compaction throughput, complete native/platform/Cluster validation or
broader comparative performance. Historical journal throughput samples remain
available and are not erased by this change.
