# Multi-shard managed ingestion: plan and evidence

Date: 2026-11-20. Source: `1b5918e6`. Status: assessment complete; runtime work deferred.
[Intent](intent.md) · [Specification](spec.md) · [Design](design.md)

## Completed

- Inspected `go/snapshot.go`, `snapshot_rows.go`, `snapshot_append.go`,
  `snapshot_compaction_copy.go` and the ownership/publication invariants.
- Read current driver issues #339/#340 and upstream Redis #16180/#16184.
- Checked official Redis Cluster and pipelining references on 2026-11-20.
- Specified alternatives, required proofs, failure handling and compatibility.
- Checked local relative documentation links and diff whitespace. No runtime
  behavior changed for #568; no server tests were needed for this documentation.

## Phases still requiring decisions and work

1. Confirm supported server/product coordination, persistence and Search contract
   with upstream; record exact releases, docs and failure semantics. An open
   feature request is not an API. Obtain design review before a custom coordinator.
2. Measure a baseline on an owned verified Software/Flex QPF4 topology: command
   processor CPU per physical primary, Search worker CPU, client conversion CPU,
   bytes/rows per shard, network RTT, SSD I/O/latency, index lag, copy/install/GC
   time and end-to-end acknowledged publication. Separate fresh load from queries.
3. If a supported primitive satisfies the design, build a fenced experimental
   layout, starting with one append generation and two real primaries; validate
   exact distribution and failure behavior before general DML/compaction.
4. Add pinned readers, DML overlays, concurrent schema work, cross-partition
   compaction/GC and durable recovery. Inject failures before/after every prepare,
   seal, publish, outcome read, reference transfer and deletion boundary.
5. Validate 1/2/4 writers and 1/2/4 physical primaries with constant total data and
   resources, warm/cold SSD controls and full independent results. Cover all 43
   queries, concurrent writers/readers, cancellation, resharding, promotion,
   restart and compatible full backup/restore. Report uneven placement and
   unsupported diagnostics. Do not claim full-scale results from a tiny fixture.

No AWS resources, operational ingestion data or backup artifacts were changed.
