# Intent: partitioned Dask reads

Date: 2026-10-20. Source: `faa03dd` plus unrelated working-tree changes.
Requestor: Albert Wong in this chat. Author: Codex. Status: implemented and locally verified.
[Issue #162](https://github.com/redis/redis-adbc-driver/issues/162),
[spec](spec.md), [plan](plan.md), [validation](validation.md).

## Problem and outcome

Dask users need a direct ADBC path to read Redis query partitions without
serializing native connections or collecting the dataset before constructing a
graph. Supply an opt-in helper and a runnable Distributed example with explicit
partition parameters and Arrow metadata.

## Scope and exclusions

Include worker-local connection lifecycle, nullable/empty partition types,
coverage verification, installation and consistency/memory guidance. Do not
change native query execution, infer partition bounds, promise snapshots or add
ADBC partition APIs. Dask's SQLAlchemy path is separate; the existing Redis
dialect means absence of direct ADBC support is not absence of all SQL access.
No publication, merge, release or human approval is implied.
