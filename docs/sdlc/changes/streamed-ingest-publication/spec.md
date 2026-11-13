# Contract

All cases use the same clean driver revision, mixed typed source and existing
managed append API. Four callers each append one disjoint 2,500-row range.
Many-call mode uses three calls per caller; stream mode uses one explicit call
per caller containing three input batches. One caller processes 10K rows.

One-stream publication intentionally changes the unit of atomic visibility and
failure. Never merge separate application calls implicitly or replay input.
The experiment does not introduce multi-statement transactions or concurrent
managed publication. Existing admitted writes serialize.

Input batches are at most 1K rows in stream/many-call modes. Source loading and
all batch objects are still materialized in each worker, so this is not evidence
of bounded application memory. Internal metadata, retained inputs and allocator
behavior are outside an input-batch-size bound.

Validate every physical type/value/count/multiplicity plus the exact ordinal set
after complete ingestion. Retain warmups, failures and all measured observations.
Verify owned Redis Software active/effective 4x QPF and strict TLS/RESP3. Setup
and final validation remain outside timing; spawned worker startup, imports,
source loading, batch preparation, connections, append and teardown are inside.

See [intent](intent.md) and [plan](plan.md).
