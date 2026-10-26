# Contracts

2026-10-22. Base `403e98f`. [Intent](intent.md), [plan](plan.md).

- DROP TABLE/VIEW lists preflight every distinct target before deleting any;
  missing names without IF EXISTS leave existing objects intact. Preserve the
  existing drop/storage-ownership behavior. This is not transactional DDL against
  concurrent changes or transport failures.
- One-dimensional scalar arrays persist element types in metadata and canonical
  PostgreSQL array text in HASH fields, remain unindexed, map to Arrow lists and
  support SQL literals/casts, subscripts/slices, comparisons, ANY/ALL, containment,
  concatenation and the requested array functions. Unsupported dimensions fail
  explicitly rather than silently flattening. Remove SQL Server bracket quoting.
- SRFs use a shared bounded/cancellable execution interface in FROM and SELECT,
  preserve lateral references, lockstep/null padding, ordinality and ROWS FROM.
  CASE/COALESCE and aggregate arguments reject SRFs. JSON SRFs belong to #254.
- Inspect and retain exact plan decisions for explicit row-ID bounds. Preserve
  residuals, ordering/limits, authoritative HASH reads, sparse/deleted IDs,
  readiness, cursor cleanup and generic fallback. Performance promotion requires
  complete full-value/multiplicity validation on the independent 100K x 105 fixture
  and narrow control, with one warmup and five alternating measured trials.
