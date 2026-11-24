# Intent

Requested 2026-11-23: address TPC-H Q19 optimizer issue [#586](https://github.com/redis/redis-adbc-driver/issues/586) while leaving full benchmark suites stopped. Base: `d68f9a21ee7a4b5a3572926ed29870c61ceb984e` (v0.0.31/current main). Status: local focused fix, not released. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Q19 repeats its join equality and shipping restrictions in three OR branches. AND-only predicate splitting leaves the equality hidden from hash-key discovery. Expose safe common conditions without rewriting the original SQL, losing duplicate matches, or crossing outer-join boundaries. Other optimizer, join-memory and Flex changes are excluded.
