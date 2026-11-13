# Validation and review

November 13, 2026. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Reproduction

The resumed four-source command-count regression fails before the change:
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

## Checks so far

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

Timing and broader regressions remain pending. Timed work waits for the
primary agent's exclusive measurement slot; no latency/throughput claim is
made from functional test durations. Raw logs, endpoints, credentials and
infrastructure identifiers remain private.
