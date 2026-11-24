# Intent

Date: 2026-11-23. Public source baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: reviewed prototype, prepared for publication; live validation pending. Requestor: repository user through the focused TPC-DS investigation. Related: [Q9 #595](https://github.com/redis/redis-adbc-driver/issues/595), [scalar residual draft #605](https://github.com/redis/redis-adbc-driver/pull/605).
See [specification](spec.md), [plan](plan.md), [validation](validation.md).

Independent uncorrelated COUNT/AVG subqueries over the same immutable managed Flex table can repeat the same physical source scan. Register compatible bound siblings without executing them, and activate one shared exact-directory scan only when normal expression evaluation first invokes a member. Preserve outer zero-row and CASE gates; never expose unused members' decode/evaluation/finalization errors early.

This is a generic algorithmic prototype, not a SQL rewrite or Q9 success claim. Full suites remain stopped. No live SQL, Redis endpoint, native-library build or container operation belongs to this prototype task.
