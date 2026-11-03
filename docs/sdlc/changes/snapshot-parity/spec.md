# Specification: snapshot feature and performance parity

Date: 2026-11-02. Status: in progress. See [intent](intent.md), [plan](plan.md).

1. A live partition set must resolve one committed generation at production.
   Independent workers retain that generation across DML, ALTER, TRUNCATE, DROP,
   producer close and compaction. Capabilities have bounded lifetime, explicit
   release/expiry and fail-closed validation; retained results remain independent
   materialized replay. Descriptors carry no endpoint or credential.
2. Optimized read paths must resolve newest versions/tombstones before SQL
   predicates, grouping, ordering, OFFSET or LIMIT. Exact integer/decimal/NULL/
   string semantics, cancellation, cursor cleanup and worker accounting remain.
3. Ordinary writes must avoid copying unrelated metadata and rebuilding unchanged
   indexes. Independent writes should publish without unnecessary conflicts.
4. Large operations need bounded batches with one atomic final publication,
   renewable ownership and controlled resource admission. Raising hard constants
   alone does not establish scalability or recovery.
5. Compaction must keep pace with sustained writes under measured workloads;
   protect references and checkpoints across takeover, DDL and failure.
6. Existing supported feature tests, native clients/platforms and Cluster API
   paths require validation. Existing skips/failures must remain visible.
7. Benchmarks require verified Software 4x QPF, isolated owned resources, stated
   baseline/source revisions, full fetch and independently checked values. No
   unmeasured all-workload or all-platform parity claim is permitted.
