# Validation and review

November 13, 2026. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Reproduction

The resumed four-source pipeline-count regression fails before the change:
two pipelines and eight directory reads, rather than one pipeline with the
same eight reads. The fixture completes an acknowledged first 64-row copy
before observing the resumed copy. The base was `34f13a21`; merged main
`0359c0d6` has an identical tree.

## Implementation contract

For immutable score-zero directories, `ZLEXCOUNT - [checkpoint` establishes
the same rank used by the previous scanner; `ZRANGEBYLEX (checkpoint + LIMIT
0 64` independently returns that rank's page. Pair the requests for at most
four sources in one pipeline, retaining the previous eight-command ceiling.
Initial pages retain rank-based reads in one pipeline. More than four resumed
sources use two pipelines. Every reply and short-page/cardinality proof is
checked before updating the temporary scanner. Nothing is persisted until
the unchanged bounded atomic copy/checkpoint script succeeds.

The small selective-job path, 64-row/1-MiB checkpoint limits, durable record
format, source references, writer fences, row images, tombstone semantics,
readiness proof and unknown-outcome handling are unchanged. No mutation or
SQL/Arrow input is replayed.

## Correctness checks

Owned Redis Software started through `make -C go redis-up`, active verified
4x QPF / six Search workers / mnp routing / 32 connections, one RAM shard,
strict certificate-verified data TLS/RESP3 and noeviction.

Focused `go test -count=1 -race -json -p 1` compaction checks: **17 passing
test events**, zero skips, failures or race reports. Coverage includes:

- The two-to-one resumed round-trip regression and unchanged eight-command
  pipeline ceiling; one/four/five/eight source sets.
- Sparse exact IDs above 2^53, initial/resumed pages and a one-row partial tail.
- Existing small-job serial reads and 64-ID request bounds, including lex pages.
- Rank/page pipeline and per-command errors without checkpoint advancement.
- Missing, truncated and malformed directories without additional row output;
  restoring the deliberate fixture corruption permits a new owner to resume
  and fully validate every value and multiplicity.
- Existing overlapping versions and sparse deletion/tombstone compaction.

Broader `go test -count=1 -race -json -p 1 -run
'^TestSnapshot.*(Compaction|DirectoryMerge)' -timeout=12m .` completed with
**107 passing test events**, four explicit skips, zero failures and zero race
reports. It covers copy atomicity/preflight/repair, source and owner fences,
index construction/readiness, queue/admission, concurrent tail grafting,
streaming retained readers, sparse deletion and selective compaction, directory
merge identity/cancellation, the 64-row/1-MiB copy bounds and a 100,001-row job
that exceeds the statement row budget. The four skips were:

- Two opt-in diagnostic profiles, deliberately disabled during correctness.
- `TestSnapshotCompactionPreindexOldWorker` and
  `TestSnapshotSelectiveCompactionOldWorker`: the preserved protocol-15 native
  library/Python-manager runtime was not provisioned. Current-worker owner
  fences and resume/recovery cases ran; old-native-worker compatibility remains
  an explicit coverage limit.

`env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -json -p 1 ./...`
passed: **1,365 passing test events**, **1,265 skipped test events**, zero
failures; six packages passed and three packages reported no test files.
Integration/capability skips in this endpoint-free run do not establish Redis
compatibility. The live Software race suite above provides the changed-path
coverage.

`gofmt -l` for the four changed Go files returned no files; `git diff --check`,
local change-record links, all 30 sample records and an operational-identifier
scan passed. The primary agent inspected the production diff without findings;
author review covered the three REVIEW.md passes. Neither is human merge
approval. The standalone Search module version was not captured; the installed
Search capability and effective QPF settings were verified.

Owned Compose container and network were removed and absence verified. Generated
environment and TLS material were removed. The pre-existing separate user
fixture remained running. Raw logs, endpoints, credentials and infrastructure
identifiers remain private.

## Controlled local comparison

[All sanitized comparison samples and hashes](measurements.json) are retained.
The baseline is merged main `0359c0d6`; candidate production source is
`6e0285b1`. Both Go 1.27.3 test binaries use identical profile helpers, differing only
in `snapshot_compaction.go`. Embedded binary build metadata confirms the module
toolchain; the outer launcher reports Go 1.27.2 outside the module. This is Go attribution, not C-ABI throughput.

An exclusive local measurement slot used Redis Software 8.2.6-25 / Redis 8.8.0,
one RAM shard, verified active/effective 4x QPF, six Search workers, mnp routing,
32 connections, noeviction and certificate-verified data TLS/RESP3. The host
was Apple M5 Max arm64 with 36 GiB memory; Docker had 18 CPUs and 25,420,967,936
bytes memory. The localhost management endpoint retained its fixture bootstrap
self-signed certificate; the data connection used the generated trusted CA.

Three compaction pairs alternated baseline/candidate, candidate/baseline,
baseline/candidate. Each used 20,000 rows × three columns, an insert plus three
dense updates, four immutable sources and an unpaced manual worker. Setup and
exact complete-result validation were outside timing. All six attempts passed;
one preceding candidate smoke is retained separately, not pooled into results.

| Compaction observation | Baseline median (range) | Candidate median (range) |
|---|---:|---:|
| Complete job | 7.727 s (7.520–7.733) | 7.320 s (7.231–7.413) |
| Directory pipeline client time | 198.0 ms (194.6–202.4) | 111.0 ms (110.4–111.2) |
| Directory pipeline execution calls | 627 | 314 |
| Directory commands | 2,508 | 2,508 |
| Copy steps / all steps | 314 / 316 | 314 / 316 |

The deterministic saving is 313 client pipeline execution calls for this
four-source shape. The first page already took one call; subsequent resumed
pages take one rather than two. For five through eight sources the bounded
split still takes two calls. These are hook-observed execution calls, not a
count of physical wire attempts. Directory time includes client scheduling,
transport and Redis execution; it is not isolated server CPU.

Two further pairs alternated baseline/candidate then candidate/baseline using
the existing publication profile: 10,000 rows × 105 columns, automatic
maintenance and 1,000- or 10,000-row append call units. Each process retained
three trials per call unit, six observations per variant/unit overall. Every
one of the 24 samples fetched and validated the entire result outside timing.

| Publication observation | Baseline median (range) | Candidate median (range) |
|---|---:|---:|
| 1,000-row append call units | 8.162 s (7.915–8.387) | 7.798 s (7.660–8.004) |
| 10,000-row append call unit | 1.747 s (1.733–1.788) | 1.742 s (1.707–1.796) |

The observed local medians are 5.3% lower for unpaced compaction and 4.5% lower
for the 1,000-row publication case. The 10,000-row control is effectively
unchanged. The small repetition count and shared workstation do not establish
a general application/native throughput gain. All comparison attempts were
retained, with zero failures, skips or caller retries. Row copy count,
checkpoint protocol, image work and directory command count are unchanged.
No CPU profile, controlled-RTT result, full ClickBench rerun or external-server
claim is included.
