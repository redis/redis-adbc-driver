# Contract and measurement

Date: 2026-10-22. Base: `aab9612`. Status: measured; review pending.
See [intent](intent.md), [plan](plan.md) and issue #442.

Use the independently typed official 100K × 105 ClickBench prefix. Load exactly
the same selected values into Redis HASHes, PostgreSQL and ClickHouse; reference
tables additionally store an unselected ordinal for disjoint four-reader SQL
ranges. The primary Redis four-reader profile uses public ADBC partition
descriptors; a separate explicit-range control can select generic execution. Reference types
preserve signed integers, strings, dates and microsecond timestamps. Compare
complete multisets, including every value and duplicate multiplicity, after
timing. Never replace the oracle with driver-produced values.

Measure uncompressed localhost TCP, identical server CPU/memory limits,
alternating method order, warm-up and repeated trials. Separate persistent
connections from fresh spawned processes. All timed reads normalize to the
same Arrow schema and write the same IPC stream format. Exclude source
conversion, load, connection creation for persistent trials, server-counter
sampling and validation. Include connection creation/imports/startup/teardown
for spawned trials. First-batch reporting records actual batch row counts;
PostgreSQL byte hints do not guarantee a row count matching other engines.

Record direct timings separately from optional TCP-meter runs. The meter runs
in another process, counts application TCP payload in both directions, adds
proxy overhead, and does not count TCP/IP headers. Server cgroup CPU counters
include the whole container and small sampling overhead; they are not an exact
query CPU split. Persistent worker RSS is process lifetime high-water memory,
including prior methods and validation, not isolated query memory.

The command-allocation screen retains HMGET count, fields/order, protocol,
page/pipeline bounds and independent commands. It shares immutable field-name
interface values and uses non-overlapping, capacity-limited argument slices.
Narrow command construction remains unchanged. Promote only after repeated
validated wide elapsed-time benefit and narrow controls; retain failed screens.

Both the command-arena screen and an additional standalone EVAL_RO bulk-read
screen failed the elapsed-time gate. Neither is promoted. Their patches and
measurements are retained; supported production source remains unchanged.
