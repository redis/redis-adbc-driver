# Specification

2026-10-21. Author: Codex; requestor: Albert Wong. Status: private reader implemented and locally verified.

Retained IPC chunks contain one schema, one record batch and an explicit end
marker. Use uncompressed, non-dictionary SQL output types (including nested
lists/records). Before Arrow decoding, check framing, bounded metadata vectors,
strings, field count/depth, record nodes/rows, buffers and exact framing. Reject
compression, extension types, oversized or malformed metadata before allocation.
Validate reconstructed arrays, schema fingerprint and exact manifest row count.
Read one chunk at a time; split record batches using stream_batch_rows. Read-only
workers require only namespace GETRANGE/TIME and normal connection discovery,
without source SQL reads or writes. Check job state/integrity between chunks.

See [intent](intent.md), [plan](plan.md).

The public ADBC API remains unchanged in this component PR. Reader-only ADBC
bootstrap and bounded coordination remain required before retained mode is enabled.
