# Specification: partitioned Dask reads

Date: 2026-10-20. Source: `faa03dd`. Author: Codex. Status: implemented.
[Intent](intent.md), [plan](plan.md), [validation](validation.md),
[issue #353](https://github.com/alberttwong/redis-adbc-driver/issues/353).

## Contract

`adbc_driver_redis.dask.read_query(uri, query, partitions, *, schema,
db_kwargs=None, conn_kwargs=None)` returns a lazy Dask DataFrame using
`dd.from_map`. `partitions` is a nonempty iterable of positional parameter
sequences. Query values use `?` bindings. `[()]` permits an unparameterized
query. Invalid schema/partition inputs fail locally without connecting.

`schema` is a required PyArrow Schema. Graph metadata is an empty pandas
DataFrame using Arrow dtypes. Each task opens and closes its own DB-API
connection and cursor, fully fetches Arrow, verifies exact schema equality
ignoring schema metadata, then converts with the same dtype mapping. Schema
mismatches fail rather than silently casting. Empty results obey the same rule.
Index divisions are unknown; indexes are local to each result partition.

The `dask` extra supplies Dask DataFrame/Distributed >=2024.5, pandas >=2 and
PyArrow >=14. Native
library discovery and DB/connection options follow the existing DB-API facade.
There are no native handles in the graph. Client/worker dependencies and Redis
reachability are deployment requirements.

## Risks and measurable checks

Callers own exhaustive, disjoint predicates, NULL handling, partition sizing and
source stability. Reads/retries have no shared snapshot. Whole partitions must
fit worker memory; projections and filters are specified in SQL without Dask
pushdown. The example uses bound half-open ranges, a dedicated NULL partition
and an empty range over a uniquely named disposable table.

Tests must check no reads during graph construction, resource cleanup on execute
and fetch errors, schema mismatch rejection, and distributed execution in two
processes. Compare the complete result independently against seeded Arrow data,
including nullable integers/strings, boundary rows, empty partitions and dtype
agreement. Existing Python checks provide regression coverage; CI includes the
new tests through the existing `python[test]` installation.
