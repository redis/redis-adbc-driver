# Specification

Date: 2026-10-30. See [intent](intent.md) and [plan](plan.md).
The normative behavior is in [Arrow interoperability](../../../arrow-interoperability.md).

| Issue | Concrete change |
|---|---|
| 198 | Stream unordered prepared-key SELECT/UNION ALL leaves through existing chunked HASH iterator; retain blocking fallback and inventory operators |
| 199 | Separate in-process C ownership from Redis wire conversion; retain lifetime copies and test retained converted batches |
| 200 | Map all uint64 to exact NUMERIC(20,0); retain signed smaller-width mappings and target-range errors |
| 201 | Keep SQL decimal normalization; explicit decimal result-width selection checks declared precision |
| 202 | Keep SQL Date64/UTC normalization; select Date64 and valid explicit zoned timestamp output |
| 203 | Select exact month/day-time/duration representation; reject calendar-to-duration or lossy unit conversions |
| 204 | Select large/view/fixed binary result layouts with stable schema and owned output buffers |
| 205 | Opt-in per-batch string/binary int32 dictionary output, 65,536-entry limit; IPC stream replacements; reject IPC files/partitions |
| 206 | Store recursive ragged lists with NULL/empty distinctions, normalize offset width to List |
| 207 | Store named structs with recursive normalized fields/nullability, exact append schemas, field access and binary-safe versioned HASH encoding |
| 208 | Normalize FixedSizeList/ListView/run-end input; explicitly reject Map and Union; document support decisions |
| 209 | Empty extension allowlist; consistently reject registered extensions and storage-field extension metadata; no implicit fallback/loading |

Result layouts are a statement output policy, not stored physical metadata.
ExecuteSchema must match actual selected batches. GetTableSchema reports canonical
storage. Both partition modes reject nonempty layout policy before publication.
Old metadata/scalar-array encoding stays readable. New nested/record metadata
requires an updated driver. Anonymous SQL records keep previous restrictions.

Risk areas: schema/value agreement, nested NULLs, binary-safe encoding, signed
range coercion, early release, late stream errors and IPC dictionary replacement.
Acceptance requires complete fetch, exact values/types and ownership checks;
benchmarks do not substitute for correctness. General Map/Union storage and
extension registration are not implemented and must not be claimed supported.
