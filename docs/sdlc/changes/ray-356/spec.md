# Bridge validation contract

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: f5e90d8. Status: locally verified.
See [intent](intent.md), [plan](plan.md), and [verification](validation.md).

Execute SQL through `adbc_driver_redis.dbapi`, fetch a complete PyArrow table,
close the ADBC cursor/connection, and construct a Ray Dataset with
`ray.data.from_arrow(table)`. Fully fetch Ray's Arrow blocks and compare schema
(including metadata), row count and values with the ADBC result.

Cover `SELECT 1`, nullable scalar types and empty query results. Also run a
normal `map_batches` operation in a real local Ray worker. Create unique Redis
fixtures and drop them afterward; shut down the test's Ray runtime.

This path materializes the result on the calling process and loads it into
Ray's object store. It is a small-data bridge, with no streaming/distributed
database read, snapshot or production performance guarantee. Those capabilities
remain outside this phase of #356.
