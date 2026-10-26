# Contracts

2026-10-22. Baseline `d72b730`. [Intent](intent.md), [plan](plan.md).

- LIMIT/OFFSET/FETCH accept row-independent numeric expressions coerced to BIGINT and bound
  parameters/casts. Resolve per execution before scan planning. NULL/ALL mean no
  limit; NULL offset is zero. Negative counts use 2201W/2201X; row variables,
  aggregates, windows and incompatible types fail explicitly. Keep MySQL LIMIT
  offset,count and literal pushdown. Schema-only planning must not evaluate counts.
- DATE(x) uses the existing DATE cast, including session-zone conversion and errors.
- VALUES is a query term usable standalone, in FROM/CTEs/subqueries and set
  operations. Resolve each column across all rows, retaining NULLs/duplicates,
  temporal/numeric types and column1... names. Reject unequal widths.
- Queries without FROM aggregate over one empty row (or zero rows after WHERE),
  using existing aggregation, HAVING, grouping sets and windows.
- Bind supported typed Arrow operations per output column only for projections
  with at least 64 outputs, matching the existing wide-scan threshold. Narrow
  projections retain generic coercion/appending. Preserve stored-field
  validation order before output coercion, exact integer width/overflow, defaults,
  CHAR/binary/temporal/decimal semantics and generic fallback. Keep cursor, row
  selection, network and batch settings unchanged.
- Performance gate: independent 100K x 105 fixture and 3-column control, regular
  and public partition reads, one warmup/five alternating measured trials. Fetch
  completely; independently validate every value and row multiplicity. Record
  elapsed ranges, first batches, CPU, allocation/memory interpretation and bytes.
