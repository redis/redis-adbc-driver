# Contract and acceptance

Date: 2026-11-06. Requestor: repository owner (user). Author: Codex. Source: `1970bf4a49d13f70f764bf647caa6dad15dc8018`. Issue: [#338](https://github.com/redis/redis-adbc-driver/issues/338). Status: implementation and comparative evidence complete; final-head delivery gates and closure are tracked on [PR #349](https://github.com/redis/redis-adbc-driver/pull/349).

Newest row versions and tombstones win before predicates, reductions, ordering and limits. Exact BIGINT, decimal, NULL and binary values survive complete Arrow and live partition reads. Identity checks, bounded pages, missing-row failures, ACLs, cursor/cancellation cleanup and worker grants retain their current contracts. No storage version, permission or wire protocol changes.

Measure narrow/wide reads, filters, aggregates, ordering/limits and live partitions. Repeat native mutable/reference/current runs in isolated processes on verified Redis Software 4x QPF. Retain all samples, client CPU, allocations and connection bytes, identifying instrumented attribution separately from native elapsed comparisons. Report negative cases without equating fewer allocations/bytes with lower end-to-end latency.

Evaluate upstream typed export and reusable Search read views; keep safe existing paths until supported capabilities exist. This issue does not promise mutable latency parity for every shape or repair the separate RDB recovery investigation #345.

See [intent](intent.md), [plan](plan.md) and [results](results.md).
