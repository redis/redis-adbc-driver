# Intent

2026-11-25; base `ed196803` (main). Requested by the TPC-H issue work for
[#652](https://github.com/redis/redis-adbc-driver/issues/652): on the 10% TPC-H subset, Q9's
parallel streaming snowflake join allocated about 6 GB per run, and GC took 40–50% of CPU
samples. Status: local change, not released. See [spec](spec.md), [plan](plan.md),
[validation](validation.md).

A CPU and allocation profile of subset Q9 on main attributes the bytes to:
- per-row decoded maps (`decodeRowCapacity`, 37%);
- a second copy of every retained dimension row (`compactDimensionRow`, 12%);
- raw HASH reply maps (`fetchRowsChunk`, 10%);
- two key strings per directory row (`nextDirectoryKeys`, 9%);
- the decimal decode path (`parseDecimal` text scanning and a same-scale `rescaleDecimal` copy, about 5%).

Remove the avoidable allocations on these paths without changing results, NULL handling, value
types, retained-memory bounds or ownership. Out of scope: replacing `map[string]Value` rows with
column-typed batches, shrinking `Value` (152 bytes, of which `ColType` is 88), and the raw reply
maps, which would need the scan iterator's sources to hand off positional rows.
