# Contract

See [intent](intent.md) and [plan](plan.md).

- Empty-result row tracing must detect HASH directory reads and ordered Search
  reads in positive controls, while preserving all zero-read assertions.
- Cursor-specific stream tests explicitly order by physical row ID; UNION
  lifecycle tests bound each unordered branch at its fixture row count. Both
  select Search while UNION remains streamable, retaining cursor existence, deletion, expiry,
  cancellation, errors and branch-transition assertions.
- Unordered single-table and UNION streams must use exact directory iterators,
  preserve complete ordered results and retained consumer buffers, and release
  their iterator and snapshot pin on completion, release, cancellation,
  connection close and later evaluation errors.
- No production implementation, workflow permissions, compatibility scope or
  storage protocol changes. No broader skips or relaxed cursor assertions.
- Validate against disposable Redis Software with active verified 4x QPF;
  keep operational artifacts and credentials outside tracked files.
