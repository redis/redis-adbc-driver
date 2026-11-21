# Implementation and validation

Date: 2026-11-20. Source: `1b5918e6`. Status: implementation and focused correctness
verification and representative mixed 105-column comparisons complete.
A 100-million-row run is not an issue #564 acceptance criterion; human approval
is not inferred from measurement results.
See [intent](intent.md), [spec](spec.md) and
[#564](https://github.com/redis/redis-adbc-driver/issues/564).

1. Define narrow eligibility and successful early-index state in a dedicated
   helper; invoke after Arrow schema/default/check preparation and before Next.
2. Teach final sealing to retain that index and skip only redundant population.
   Do not modify compaction or retained-operation formats.
3. Add eligibility and native ordering/fault/isolation regressions; run unit
   checks and focused dedicated Software/QPF4 RAM and Flex fixtures.
4. Record actual checks, skips, review findings and remaining performance gaps.
   No full ClickBench reload or AWS resources. Publish a draft PR for review;
   do not merge.

## Evidence

The sanitized [matched performance record](evidence.md) contains the coordinating
agent's clean twelve-observation Linux/Flex comparison and explicit resource and
full-scale limits. The checks below are the original focused Go correctness
evidence; they do not imply that the broader integration suite has finished.

Verified dedicated Redis Software RAM and Flex fixtures (database reported Redis 8.8.0): active QPF4, six
Search workers, mnp routing, 32 connections, TLS, noeviction, one shard. Fixtures
were configured through `make -C go redis-up` and management policy verified by
the coordinating agent. Fixture identities and raw evidence remain private.
Client: macOS ARM64 through Docker's published port; these are correctness
observations, not transport-controlled throughput comparisons.

- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_SOFTWARE_URI go test
  -count=1 -p 1 ./...`: all packages passed; native suites intentionally skipped.
- RAM `go test -count=1 -p 1 -timeout 8m -run
  '^TestSnapshotAppend(Index|Indexed)' .`: passed, 3.026s.
- Flex same selector with `REDIS_SNAPSHOT_FLEX=true`: all cases executed and
  passed, 4.212s. An earlier invocation with an unset snapshot endpoint skipped
  native cases and was discarded, not counted as evidence.
- RAM `go test -race -count=1 -p 1 -timeout 8m -run
  '^TestSnapshotAppendIndexed(ParallelStreams|StagedCancellation|FaultIsolation)$'
  .`: passed, 3.934s, no race report.

Coverage includes four overlapping same-table streams, reverse-order CAS/rebase,
failed peer, old/intermediate pinned roots after GC, staged cancellation, source
rows/private index isolation, creation denial/lost reply before input, dropped
index, durable resolution of a lost successful row reply, proof warnings and
transport errors, typed proof-permission fallback (RAM) or refusal (Flex), and
4096 rows × 105 columns with exact values above 2^53 and NULLs. Runtime public
indexed-column fallback retains the notification pass; unit eligibility tests
exclude dynamic public TAG/NaN and provenance TAG layouts. Flex PlainIndex
retains hidden numeric row/deletion fields while omitting SORTABLE.

## Matched performance procedure

`go/snapshot_append_index_profile_test.go` depends only on helpers present at the
source revision, so it can be copied unchanged to a baseline worktree. Build
both static Linux ARM64 test binaries. Run each in the same Software container
network namespace, after verifying QPF and empty dedicated fixture state, with
`REDIS_APPEND_INDEX_PROFILE=true`, `REDIS_APPEND_INDEX_PROFILE_ROWS=16384`,
`REDIS_APPEND_INDEX_PROFILE_WRITERS=1` or `4`, and an explicit
`REDIS_APPEND_INDEX_PROFILE_ROUTE` label. Test selector:
`-test.run '^TestSnapshotAppendIndexProfile$' -test.v -test.count=1`.

Use at least three alternating baseline/feature observations for each writer
count, recording source revisions and available server/client CPU, RSS and I/O;
record unavailable metrics explicitly instead of treating them as zero.
The profile reports total acknowledged wall time and per-worker input, row-write
wall time, create/notification/proof/publication command durations. Concurrent
command durations overlap and cannot be summed as wall-time phases. Full
105-column exact-value/NULL/unique-ID validation is outside timing and mandatory.
Synthetic BIGINT fixtures do not replace canonical ClickBench measurements.

The coordinating agent completed this bounded procedure: all twelve full result
oracles passed, with final notification commands reduced from 64 to zero per
run. The [evidence record](evidence.md) reports acknowledged-time medians and
ranges, measured client observations, unavailable server/I/O attribution and the
discarded image-pull-overlap matrix. No 100,000-row profile or full-scale result
is implied by the 16,384-row observations. This paragraph describes the earlier
BIGINT-only control; later mixed-data closeout is linked below. Implementation
was merged in PR #569; no separate formal human review decision is invented.

See [combined verification](integration.md) for final integrated checks and limits.

Product-version wording is clarified in the [measurement provenance erratum](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/provenance-erratum.md); historical timings and settings are unchanged.

## Representative mixed-data measurement closeout

The [2026-11-20 measured closeout](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/issue-564.md) supplies three alternating serial/four-writer baseline/current observations for independently typed canonical mixed IPC and streamed 262,144-row data, exact all-105-column value validation, positive command-phase observations and explicit CPU/RSS/I/O scopes. See its per-criterion checklist; #566's separate full-canonical ingestion gate is not imposed on this issue.
