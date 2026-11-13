# Contract

Status: implemented and locally validated; see [validation](validation.md). See [intent](intent.md), [plan](plan.md).

- Append only to an existing unmanaged HASH/foreign-index registration, with
  explicit non-NULL string `__key` values and at least one non-NULL stored field.
- Reuse SQL insert key ownership, duplicate/existing-key refusal, source index
  validation, field mapping, native/text encoding and type/default checks.
- Reject create, replace, create_append, filtered foreign indexes, retained
  preparation, staged writes and shared read snapshots before input consumption.
- Pin catalog metadata; perform no managed workspace staging/publication or
  managed ingest serialization. Source values remain live.
- Convert bounded slices of streamed Arrow batches, consume once, and never
  automatically replay source writes. Rows are individually atomic; the stream
  can partially succeed. Cancellation, conversion/reader errors, key conflicts
  and unknown replies stop immediately.
- On error report acknowledged row count as a lower bound, preserving ADBC
  status/SQLSTATE. An uncertain current mutation may also have applied; no
  rollback, upsert, whole-stream atomicity or automatic retry is promised.
- DROP/GC retain application HASHes, TTLs and indexes. No managed NativeClient
  changes or performance claims are included.
- No storage protocol or registration-format change is required; existing SQL
  and ordinary Redis clients continue to read the same application HASHes.
