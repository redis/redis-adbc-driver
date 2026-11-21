# Bounded compaction throughput: plan and evidence

Date: 2026-11-20. Source: `1b5918e6`. Status: implementation and focused validation complete; broader acceptance remains.
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

## Remaining issue acceptance

Full representative wide concurrent four-Arrow-writer measurement, physical
command/Search/client CPU attribution, SSD I/O, backlog and reclamation lag are
not established by a bounded isolated control. Do not close #565 or claim a
full-scale ingestion speedup on this evidence alone.
