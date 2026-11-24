# Intent

Requested 2026-11-23: repair analytical benchmark isolation, add DuckDB-over-Redis-ADBC, test that route live, and run only TPC-H Q19 while keeping full suites stopped. Base `d68f9a21ee7a4b5a3572926ed29870c61ceb984e`; not released. See [spec](spec.md), [plan](plan.md), [validation](validation.md), [#596](https://github.com/redis/redis-adbc-driver/issues/596).

Old workers retained DuckDB caches and reference results while timing Redis under the same client cap. Separate engine/validation lifetimes and failure attribution; retain historical limitations rather than reclassifying worker OOMs as isolated driver measurements. Driver optimizer changes are in a separate PR.
