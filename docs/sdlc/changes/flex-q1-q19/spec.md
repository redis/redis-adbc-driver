# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

**Flex partitioned aggregate** (`go/partitioned_aggregate.go`). With Flex, `partitionedAggregate` now runs for managed Flex snapshot tables. Other Flex sources keep the serial aggregate, as do plans without an original predicate whose Search query cannot be recovered.
- **Exact-directory partitions:** each row-ID partition reads its exact directory range (`flexDirectoryWhere`: no Search query, the complete original predicate as the residual).
- **No Search fast paths:** server-side tuples, compact integer probes and Search visibility queries stay off. Exact directories resolve newest versions and tombstones, including in independently invoked partitions.
- **Residual on rows:** because the residual is checked on rows in the scan, the server-tuple requirement that residual inputs be tuple columns (`distinctResidual`) does not apply. Q1's `d <= DATE … - INTERVAL '90' DAY` is accepted.
- **Unchanged:** the partition merge, exact decimal partial sums, ordering and fallback rules. RAM databases keep every existing path.

**Flex eager-join inputs** (`go/join.go`, `go/join_snowflake.go`). The parallel chunk reader behind `scanParallelDimension` is now a generic `parallelTableRows`, with `parallelTableEligible` for its source checks. With Flex, `scanJoinItem` reads an eager join's input through it when every one of these holds:
- **Table:** an eligible published managed table of at least `snowflakeParallelDimensionRows` (65,536) rows.
- **Plan:** no direct keys, limit, sort, late projection, ties, dependencies, visibility queries, hash scan or authoritative read, and not lateral.
- **Predicate:** one the parallel readers accept.
- **Presence tag:** the input's presence tag is not read by the query.

Inside that reader:
- **Shared candidates:** a Search filter within the shared candidate budget reads its candidates split across workers.
- **Ranges:** a filter over the ordinary budget, or a broad one, reads exact-directory ranges.
- **Everything else:** keeps the serial scan.

The complete predicate is checked on every row, rows arrive in row-ID order, and the input returns no keys, so its presence tag is true, as for in-memory inputs. The reader now records its worker grant.

Acceptance:
- **Aggregate:** `TestSQLFlexPartitionedAggregate` (Flex, 197,608 rows, Q1-shaped exact decimal and integer aggregates with Q1's date-minus-interval filter) reports `partitioned_exact`, no server or compact tuples, at least 2 workers and exact results. It fails on main (serial aggregate), and fails without the residual change (declined by `distinctResidual`).
- **Eager join:** `TestSQLFlexEagerJoinParallelInputs` (a Flex Q19-shaped eager join with NULL and missing keys) returns the serial read's result, with a worker grant of at least 2 while the plan stays the eager join. It fails on main (grant 0).
- **No regressions:** the existing star, snowflake, partition and aggregate tests and the snapshot suites pass.
- **SF1 Flex:** Q1 and Q19 fully fetched and validated, with times recorded.
