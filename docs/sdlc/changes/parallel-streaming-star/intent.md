# Intent

Date: 2026-11-23. Status: unit-verified publication candidate; combined Q7 validated, Q4 still times out.
Origin: follow-up to [draft PR607](https://github.com/redis/redis-adbc-driver/pull/607),
related to [TPC-DS Q4 issue590](https://github.com/redis/redis-adbc-driver/issues/590).
See [specification](spec.md), [plan](plan.md), [verification](verification.md).

Bounded grouped star streaming avoids complete fact/join materialization, but
the focused Q4 pilot still exceeds its warm-up deadline. Read and probe broad
published native facts concurrently within the existing shared worker budget,
while retaining a single original exact grouped reducer and bounded row queues.
No full benchmark, native build, SQL endpoint or container operation is part of
this implementation. No successful latency or issue-resolution claim is made.
