# Bounded compaction throughput: plan and evidence

Date: 2026-11-20. Source: `1b5918e6`. Status: implementation, focused failure/native checks and scoped isolated/concurrent measurement acceptance complete.
[Intent](intent.md) · [Specification](spec.md) · [Evidence](evidence.md)

## Implementation

- Keep protocol-17 atomic batch/outcome bounds unchanged. Larger 128/256-row
  *atomic* candidates were rejected before testing because old workers reject
  their persisted outcomes. Compare equivalent maximum 64/128/256 rows per
  maintenance step through one/two/four compatible atomic batches instead.
- Add root-aware bounded copy bursts in `go/snapshot_compaction.go`.
- Add focused DDL-between-batches, cancellation, sparse overlays, deleted-prefix
  and lost-reply checks; retain existing takeover, pending-copy repair, old reader,
  byte-bound, prefix, source-change, EOF/index and newer-tail tests.
- Add explicit opt-in 4096-row, 105-column NOINDEX profile with four disjoint
  append segments, NULL strings, exact integers, complete fetch and every-value
  validation. It times copy/index/install separately via existing command hooks.
  Setup and final fetch are excluded. This is not canonical ClickBench or a
  concurrent writer workload, and does not establish full-scale throughput.

## Checks

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`: PASS;
  native endpoint tests skipped, no Redis compatibility inferred from this pass.
- Native Software RAM and Flex QPF4 full focused compaction suites: PASS,
  42 top-level checks each, 5 explicit profile skips each.
- Linux-network-namespace Flex 105-column candidates: all nine complete
  profiles PASS every-value validation; see [measurements and limits](evidence.md).
- `gofmt`, `git diff --check`, local documentation link checks: PASS.

## Concurrent measurement closeout

The [six repeated-wave controls](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/issue-565.md)
add representative canonical 105-column, three-wave/four-writer native workloads,
exact every-value/multiplicity validation, CPU/RSS/I/O windows, sampled copy overlap,
backlog and normal-censored/explicit reclamation measurements. Common settings,
source and the original four/eight thresholds are retained. Current/base includes
#564 as well as #565; isolated one/two/four-batch controls distinguish the burst's
scoped contribution. Short concurrent index/install transitions and Search-only
CPU are explicitly unavailable, not invented. Full canonical 100M/backup remain
separate #566 limits, not an added gate on #565.
