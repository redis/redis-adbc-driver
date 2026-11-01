# Contracts and acceptance mapping

See [intent](intent.md), [verification](plan.md), and the
[public contract](../../../arrow-interoperability.md).

| Issue | Closure contract and evidence |
|---|---|
| #198 | Keep ordinary safe fallback; add opt-in `stream_required` to reject blocking/materializing plans before execution. Support unordered managed key/index leaves and UNION ALL. Compare complete schemas/values/duplicates/NULLs/coercions/node limits, late errors, cancellation/early release and existing cursor/worker cleanup. Record repeated first/full time, allocations and sampled heap across counts/cardinalities. |
| #200 | Full uint64 domain maps exactly to NUMERIC(20,0); smaller widths widen to signed SQL types. Check all width boundaries, signed maximum + 1, UINT64_MAX/NULL, mixed arithmetic/aggregates/comparisons and signed append overflow, without float storage. |
| #201 | Input decimal widths normalize to NUMERIC precision/scale; output widths are explicit. Check all four widths at extrema and negative/zero/positive scales, coefficient equality, promotion/rounding/overflow, schema agreement and SQL numeric-domain scale bounds. |
| #202 | Date64 floors to signed int32 epoch days; timestamp zones normalize to UTC with explicit output metadata. Check all time/timestamp units, pre-epoch/non-midnight values, named DST-fold instants, numeric offsets, invalid zones/ticks and extremes; distinguish TIMETZ. |
| #203 | Preserve calendar months/days/nanoseconds exactly. Duration input checks overflow; explicit unit/interval output rejects calendar loss, non-divisibility and range overflow. Existing legacy interval metadata remains canonical MonthDayNanoInterval. |
| #204 | Explicit large/view/fixed buffer output has stable physical schemas and owned retained batches. Validate sliced/empty/NULL/binary-NUL data, width errors and synthetic huge offsets; distinguish payload budgets from offset capacity/RSS. |
| #205 | Decode input dictionaries with NULL indices/values and unused entries. Output supports String/Binary per-batch int32 dictionaries, bounded by 65,536 entries and 16 MiB unique payload; reject over-budget input rather than switch schema. IPC streams replace dictionaries; files and selected-layout partitions reject. |
| #206 | Ragged recursive lists, NULL/empty/NULL-element distinctions, nonzero slices, canonical List offset policy, indexing and append range errors; no rectangular SQL dimension declarations or nested Search indexing. |
| #207 | Named Struct metadata, recursive bounds, nullability and field rules persist. Append rejects schema mismatch before row processing even for empty/all-NULL batches. Preserve anonymous ROW restrictions, nested/sliced data and exact child schemas. |
| #209 | Default rejects extensions. Registry revision 1 explicitly normalizes top-level arrow.uuid/arrow.json storage when requested; unknown/custom/nested semantics reject. Validate metadata bounds/duplicates, storage mismatch, JSON values and evolution policy; changing policy after schema discovery rechecks the original schema. No metadata-driven plugin execution. Canonical storage/output/replay carries no extension identity. |

The shared matrix constructs independent Arrow expectations and checks full,
nonzero-offset and empty input through create/append and Bind/BindStream. It
compares exact canonical schemas and values after fresh metadata reload, native
C parameter binding, IPC streams/files, and independent live/retained replay.
Retained replay is tested after truncating live source rows. Unordered SQL
compares complete row multisets, preserving duplicates/NULL/nested shape;
ordered queries and IPC remain order-sensitive. Explicit decimal layouts test
ExecuteSchema and actual batches with both partition modes' rejection behavior.

Limits are scoped, not universal RSS guarantees. The streaming policy excludes
binding/key-list memory, server work and retained consumer buffers; ordinary
blocking operators remain plan-dependent. Dictionary limits exclude bookkeeping
and index/source/consumer buffers. Extension fallback is opt-in, top-level only,
and does not preserve logical extension type or metadata in SQL storage.
