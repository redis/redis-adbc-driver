# Contract

Date: 2026-10-29. Source baseline: `c664eb1`. Author: Codex.
Status: locally verified. See [intent](intent.md) and [plan](plan.md).

The integration uses the existing autocommit DB-API facade. Each Ray SQL task
opens its own connection through a serializable factory. No native driver
changes or new mandatory dependencies are needed.

Acceptance checks fully consume results and independently compare expected
rows: parameterized SQL filtering with NULLs, grouped aggregates, a 300-row SQL
write spanning Ray's 128-row executemany boundary, and Arrow ingestion/fetch
followed by a Ray Arrow batch transformation. Arrow interchange also compares
the fetched table with its source, including types and NULLs.

Tests require `RUN_RAY_TESTS=1`, `ray[data]` and `REDIS_URI` pointing to
disposable Redis with Search. Each table has a unique name and cleanup in a
finally block. Shut down the local Ray runtime at module teardown. Default
test runs skip these tests without importing Ray or starting processes.

Limits: one SQL read task, materialized Arrow fetch, autocommit writes with
possible duplicates/partial writes on retries, no validated multi-node
execution, SQL read sharding or performance claims. Ray can combine input
blocks, so two write input blocks do not establish concurrent write execution.
