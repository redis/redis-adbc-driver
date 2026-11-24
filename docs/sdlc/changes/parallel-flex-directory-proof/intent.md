# Intent

Date: 2026-11-23. Status: unit-verified publication candidate; separate combined Q7 observation recorded below.
Origin: bounded grouped streaming from [PR607](https://github.com/redis/redis-adbc-driver/pull/607)
and the independent complete-candidate cap from [PR608](https://github.com/redis/redis-adbc-driver/pull/608).
Related to [TPC-DS Q7 issue593](https://github.com/redis/redis-adbc-driver/issues/593).
See [specification](spec.md), [plan](plan.md), [verification](verification.md).

A compiled selective Flex predicate may exceed complete-result count or key-byte
budgets and therefore use the exact directory path. Permit the bounded parallel
fact reader only after proving that planning outcome once at the coordinator.
Keep genuinely admitted selective queries on their existing candidate path and
never repeat candidate enumeration per worker. No benchmark query is changed.
