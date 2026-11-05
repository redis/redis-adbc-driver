# Spike: Redis Flex support with plain NUMERIC fields

Date: 2026-11-05

Status: Investigation proposed; no driver changes or new live Flex tests performed.

## Question

Can the driver support Redis Flex using `NUMERIC` fields without `SORTABLE`,
and what would that mean for functionality, query performance, and memory?

`SORTABLE` is a field option, not a field type. The comparison is
`NUMERIC` versus `NUMERIC SORTABLE`; a field cannot be declared as just
`SORTABLE`.

## Initial finding

Plain `NUMERIC` is sufficient for numeric equality and range filtering.
`SORTABLE` additionally stores values for efficient access during sorting
and aggregation, avoiding reads of the original documents for those values.
That costs additional memory. It does not inherently improve numeric range
filtering.

| Concern | NUMERIC | NUMERIC SORTABLE |
|---|---|---|
| Numeric equality and range filters | Supported | Supported |
| Stored value for sorting and aggregation | Requires loading the value or another execution path | Available from the sorting vector |
| Current driver behavior with an adopted application index | Pushes down numeric filters; sorts and aggregates in the driver | Eligible numeric sorts and aggregates can also run in Redis |
| Memory | No additional sortable-value storage | Additional storage for sortable values |
| Expected performance tradeoff | More document reads or client work for sorts and aggregates | Can reduce document reads and data transfer |

These are execution tradeoffs, not a claim that SQL correctness requires
`SORTABLE`. Plain numeric values can be loaded and processed correctly by
another execution path. Performance and memory differences require measurement.
See Redis's [field options](https://redis.io/docs/latest/develop/ai/search-and-query/indexing/field-and-type-options/)
and [sorting documentation](https://redis.io/docs/latest/develop/ai/search-and-query/advanced-concepts/sorting/).

## Current driver dependencies

- [`indexCreateArgs`](../go/store.go) always declares
  `__rowid NUMERIC SORTABLE`. Removing support for `SORTABLE` therefore breaks
  index creation for driver-created tables.
- [`indexAttrArgs`](../go/store.go) also declares indexed numeric columns as
  `NUMERIC SORTABLE`; changing only the row-ID declaration is insufficient.
- [`appindex.go`](../go/appindex.go) and [`inplace.go`](../go/inplace.go)
  already distinguish plain numeric attributes from sortable ones in adopted
  application indexes. This provides an existing fallback model, but does not
  establish that driver-created tables work without sortable fields.
- [`tags.go`](../go/tags.go) gates pipeline eligibility through `inPipeline`.
  Driver-owned columns currently assume that their index fields support the
  pipeline. A Flex path must represent reduced capabilities explicitly.
- [`query.go`](../go/query.go) uses the row ID for default ordering on relevant
  query paths. Its projection, numeric conversion, ordering, and aggregation
  assumptions need review before changing index declarations.
- Row-reading code uses `FT.AGGREGATE ... WITHCURSOR`. Cursor availability is
  a separate requirement from numeric field support.

## Flex compatibility boundary

As checked on 2026-11-05, the Redis Software Flex documentation lists
`NUMERIC` as supported, but excludes `SORTABLE`, `UNF`, and aggregate cursors.
It also excludes `FT.ALTER` and `FT.DROPINDEX ... DD`. Consequently, removing
`SORTABLE` alone would not make this driver compatible with Flex.
Source: [Redis Software Search on Flex](https://redis.io/docs/latest/operate/rs/flex/search/).

The existing [compatibility report](redis-flex-compatibility.md) records live
tests from 2026-10-06 against an older build that rejected `NUMERIC` and
`FT.AGGREGATE` as well. Preserve those results as version-specific evidence.
Current documentation is not proof that a particular deployed build supports
the required commands. Redis Cloud and Redis Software must be evaluated
separately.

## Investigation plan

1. **Probe the target deployment.** Record Redis Software or Cloud product,
   database version, and Search version. Test plain `NUMERIC`, `SORTABLE`,
   `FT.AGGREGATE`, field loading, numeric ordering, and cursors separately.
   Use isolated temporary keys and indexes and remove them afterward.
2. **Prototype capability-aware schemas and planning.** Omit unsupported field
   options for Flex and reuse appropriate filtering and client execution paths.
   Do not let driver-owned columns automatically qualify for operations that
   need unavailable sortable values.
3. **Choose a bounded row-reading strategy without cursors.** Evaluate
   key-based pagination using numeric row-ID ranges and supported search or
   aggregate commands. Verify ordering, ties, adopted keys, cancellation, and
   behavior under concurrent writes. Do not assume offset paging is stable.
4. **Check numerical correctness.** Compare numeric ordering (including 2
   versus 10), NULL and missing fields, large integers, decimals, and supported
   special floating-point values. Check value loading and conversions before
   enabling any server-side aggregation path.
5. **Benchmark the alternatives.** Compare a sortable baseline on a server
   that supports it with plain numeric execution. Include selective range
   filters, full scans, `ORDER BY ... LIMIT`, and grouped aggregates. Measure
   latency, throughput, rows and bytes transferred, Redis CPU, document reads
   where observable, client peak memory, and Redis index/sortable memory.
   Keep datasets and deployment conditions comparable and disclose differences.
6. **Inventory remaining compatibility work.** Cover schema changes, deletion,
   string index options, and initial indexing before claiming general Flex
   support. Define any deliberately restricted initial feature set.

## Exit criteria

- Demonstrate correct filtering, projection, ordering, and aggregation without
  sortable fields on the target Flex deployment.
- Demonstrate bounded reads without aggregate cursors and document consistency
  behavior during concurrent writes.
- Quantify the memory savings and query costs for representative workloads.
- Produce a scoped implementation recommendation, capability checks, and an
  explicit list of unsupported operations.

## Working recommendation

Treat `NUMERIC SORTABLE` as a dependency of the current driver implementation,
not a fundamental requirement for SQL support. Investigate plain `NUMERIC`
with explicit capability checks and fallback execution. Keep efficient
sortable execution available on deployments that support it. Do not enable
Flex based solely on successful numeric index creation or `FT.AGGREGATE`;
the row-reading and remaining schema capabilities must also be validated.
