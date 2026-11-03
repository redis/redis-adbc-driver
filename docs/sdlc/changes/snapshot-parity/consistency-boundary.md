# Proposed shared snapshot-consistency boundary

Status: architecture recommendation, not an implemented extension or a parity
completion claim. Preserve the always-on [snapshot contract](../../../snapshots.md).

Use one driver-owned consistency layer for SQL, Arrow ingestion, catalog reads,
external-copy refresh and partition readers. It owns generation acquisition and
renewal, private-output admission and sealing, publication outcomes, reference
lifetime and reclamation. Execution paths plan and produce data through that
layer; they must not implement separate visibility or publication rules.
Compaction shares those invariants while retaining its durable job phases.
One contract does not require one file or one unbounded Redis script.

This boundary adds no optional consistency mode, multi-statement transactions or
atomic capture of externally written data. Partition children retain the
producer generation; managed external copies change only on explicit refresh.

A required Redis server extension is a separate proposal. First demonstrate a
measured bottleneck that bounded server operations cannot address and verify
Software deployment, Cluster routing, Search coordination, replication, failover
and persistence recovery. Moving code server-side alone does not establish
cross-shard atomicity or remove index construction costs. Continue current parity
work without adding that dependency.

Next implement incremental descriptor-to-data references and publication based
on actual metadata read/write sets, following the
[reference/publication design](row-reference-design.md). Verify the shared
guarantee through every public execution path, including uncertain publication
outcomes and readers surviving reclamation.
