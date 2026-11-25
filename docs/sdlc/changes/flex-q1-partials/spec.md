# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

**In-memory partial groups for Flex partitions** (`go/partitioned_aggregate.go`).
- **Old path:** a Flex partition's row scan turned every row into a partial tuple. Each of the tuple's source values was encoded as a string, and its partial sums were formatted as strings. The tuple was buffered in a spill shard (up to 65,536 tuples or 8 MiB), and the buffer was later sorted and combined by parsing those strings back.
- **New path:** for partial groups without DISTINCT, the scan now combines each tuple into its partition's in-memory group by key (`partialGroupCombiner`), with the plan's exact `combine`. Each group is handed to the spill once.
- **Bound:** at most 4,096 groups (`flexPartitionGroups`) are held; adding another key first hands the held groups on.
- **Exactness:** decimal partials, counts, MIN/MAX, NULL contributions and the complete residual predicate (checked by the scan) are unchanged. The group's representative source values are its first row's, which the spill already allowed, since its sort is unstable.
- **Metrics:** the scan counts the same rows and value bytes, reusing the tuple's encoded values instead of encoding each value a second time.
- **Unchanged:** RAM plans, and Flex partial groups with DISTINCT.

**Worker cap.** Not changed. The 32-worker Flex default and the 1–32 `query_workers` range stay. See [validation](validation.md#sf1-flex) for the measurements.

**Server-side aggregation.** Not enabled; it can't be made exact on Flex.
- **No aggregate cursors:** Flex's plain-index contract excludes aggregate cursors and SORTABLE fields, and snapshot reads use exact row-ID directories.
- **No exact decimal sums:** Search reducers sum in floating point. The exact RAM path depends on proven per-segment visibility masks, which Flex snapshots don't provide.
- **No data reduction:** grouping by Q1's six source columns yields nearly one tuple per row.

Acceptance:
- **Combiner:** `TestPartialGroupCombinerMatchesSequentialCombine` checks 5,000 tuples over 9 keys, with mixed-scale decimal partials, NULL contributions, MIN and a group limit of 3. Every group equals the sequential combine, and fewer tuples than rows are handed on.
- **Existing exactness tests:** `TestSQLFlexPartitionedAggregate`, the partitioned and decimal integration tests, and the Flex and partitioned snapshot tests pass.
- **SF1 Flex:** Q1 fully fetched and validated, with warm-up plus three timed runs at 60 s, and cold behavior recorded.
