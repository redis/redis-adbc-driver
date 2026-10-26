# Specification: native IPC

Date: 2026-10-22. Author: Codex. Status: implementation.
Source: `9493c647aee33916cfc32840b9ba517388e51b50`.
Linked [intent](intent.md), [plan](plan.md), and [#227](https://github.com/alberttwong/redis-adbc-driver/issues/227).

The [public contract](../../../arrow-ipc.md) defines statement options, formats,
paths, cleanup, cancellation and autocommit behavior. Use existing standard C
SetOption and ExecuteQuery with NULL output, implemented by native Go IPC
readers/writers. Ordinary Arrow result queries and partition execution reject
active IPC paths; disabled paths preserve their previous behavior.

Export validates a single SELECT and a fresh destination before query execution.
Its writer consumes each batch before the next fetch. Import feeds a regular
file into existing bulk ingestion, releasing the native reader/file on every
exit. File metadata can grow with batch count; malformed input can allocate
large buffers. Concurrent file modification is outside the contract. Failed
imports can leave committed rows. Export does not publish atomically or fsync.

Verification covers independent PyArrow decoding/encoding through the compiled
C library, scalar types/nulls/large integers, empty/multi-batch results, schema
round trips, cancellation, reader release, late errors and destination safety.
Checked Go allocators detect leaked buffers. No new SQL operator streaming or
performance improvement is claimed; historical measurements are labeled.
