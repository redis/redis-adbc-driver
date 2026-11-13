# Measurement contract

Use the unchanged 156-trial matrix: 84 complete reads, 40 existing-table writes
and 32 creation workloads, including labeled warmups. Read 100K rows × 105
columns; write 10K rows with 1K public-call/pipeline units. Preserve the typed
input, complete independent type/value/multiplicity validation, all attempts,
timing boundaries and concurrent managed admission semantics.

Use owned Redis Software with verified active/effective 4x QPF, six Search
workers, mnp/32 connections, one 4-GiB noeviction RAM shard, four-CPU/eight-GiB
container limits and certificate-verified data TLS/RESP3. Keep operational
artifacts private; remove only owned fixture resources and generated secrets.
Compare complete revisions across time without attributing every difference to
one PR. Neither rejected policy is enabled by this rerun.
