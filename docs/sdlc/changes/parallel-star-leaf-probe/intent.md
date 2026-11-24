# Intent

Date: 2026-11-23. Status: local candidate; native validation pending.
Origin: follow-up to [PR607](https://github.com/redis/redis-adbc-driver/pull/607),
related to [TPC-DS Q4 issue590](https://github.com/redis/redis-adbc-driver/issues/590).
See [specification](spec.md), [plan](plan.md), [verification](verification.md).

The bounded parallel star probe copies each partially joined row into a new map
at every dimension. That allocates intermediate maps even when a later dimension
has no match. Reduce those allocations without increasing memory limits or
changing source reads, aggregate evaluators, worker grants or supported plans.
A separate native query validation is required before any Q4 performance claim.
