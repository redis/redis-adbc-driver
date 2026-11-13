# Measurement contract

See [intent](intent.md) and [plan](plan.md). Status: measured; evidence in validation.md.

Build the exact v0.0.26 source in an isolated checkout with the Makefile version
linker flag; verify ADBC GetInfo. Use a dedicated Software RAM database, verified
4x QPF/six Search workers/mnp/32 connections, TLS/RESP3 and noeviction.

Reads: independently typed 100K x 105 source; project identical public columns.
Add one ordinal field for common disjoint SQL ranges. Managed data uses private
snapshots; unmanaged and redis-py share the exact application HASHes/index.
Use one/four readers, one/eight tasks, 512/4096 Arrow batch ceilings, six measured
repetitions after warmup. Include fresh process startup, connections, full fetch,
normalization, IPC output and shutdown. Validate all values/types/multiplicities
outside timing. First-batch timing is producer-side before IPC.

Writes: matched 10K x 105 public values plus ordinal, one/four writers, up to
1000 rows per SQL INSERT or native HSET pipeline, three repetitions after warmup.
Unmanaged INSERT additionally requires explicit source keys. Setup/registration
are untimed; include process/input conversion, SQL construction, connection,
write execution, shutdown and index readiness. Managed statements publish
atomically; unmanaged/native writes do not acquire managed snapshots. Validate
every key/value and exact multiplicity after each trial.

Do not pass Arrow ingestion or live partition timings off as supported unmanaged
operations. Verify the unmanaged Arrow-ingestion refusal and use common SQL
ranges for concurrent reads. Never mutate published managed HASHes directly.
