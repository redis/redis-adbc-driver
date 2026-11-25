# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Retention and spill trigger

`streamJoinPages` counts each dimension's retained rows. For a snowflake shape (and unless
`testNoJoinSpill`), the dimension whose row takes the retained total past `dimensionCap()` is
spilled:
- A directory from `os.MkdirTemp` gets `joinSpillPartitions` (64) partition files.
- The rows the dimension already retained move to the partitions. Its count leaves the
  retained total, and its buckets are dropped.
- That row and the dimension's remaining rows are written to their partitions instead of being
  retained. A row with a NULL key is skipped, as it is for buckets.

A partition record is the row's bucket key, then its compacted fields (`compactDimensionRow`),
in `spillRowCodec` format. `spillRowCodec` is now shared with the sort spill; its run format is
unchanged.

## When it still falls back

The join falls back to the eager plan before any fact row is read, with plan reason
`filtered_dimension_cap` or `spilled_partition_cap`, when:
- a second dimension also takes the retained rows past the cap; or
- the largest dimension partition plus the retained rows of the other dimensions would exceed
  the cap.

## Parallel dimension reader

`parallelTableRows` stops its workers at the row limit with a flag. Each worker's `scanPages`
ends normally, closing its cursor on the live context. It no longer cancels the shared child
context, which interrupted the execution's connections. Other errors still cancel.

When the parallel reader reports the cap for a dimension and no dimension has spilled yet, the
dimension is read again through the serial `scanItem` reader. That reader hands pages to the
spilling `keep`. The parallel reader holds a whole dimension before `keep` sees it, so it cannot
spill.

## Joining the partitions

The spilled dimension's buckets are nil while the fact's key demand is computed
(`starFactDimensionDemand` skips such dimensions), and the parallel fact reader is not used.
`joinSpilledFacts` then:
1. Writes each fact row, read by the serial reader, to the partition of its key for the spilled
   dimension. A NULL key cannot join and is dropped, as the probe drops it.
2. For each partition that holds fact rows: loads its dimension rows as that dimension's
   buckets, and runs its fact rows through the unchanged `probe`. That covers every dimension in
   order, the residual, output pages and `visit`. It stops when the consumer has failed.

Output rows are the same multiset as the eager join's. Their order is unspecified, as for every
streamed join. The directory is removed when `streamJoinPages` returns, on every path.

## Acceptance

- `TestSnowflakeBranchSpill` sweeps caps 8–200 and 1 / 4 / 16 partitions over the Q3 / Q5 /
  Q7 / Q9 / materialized-row shapes. Every run equals the eager join, at least one per shape
  joins a spilled dimension, and no directory remains.
- `TestSnowflakeBranchSpillFallsBackBeforeFacts` checks that a one-partition spill and
  `testNoJoinSpill` fall back before facts.
- `TestSQLSnowflakeJoinsManaged/cap_fallback` fails on main (`use of closed network
  connection`) and passes. `/spilled` equals the eager join at caps 64 / 400 / 1,500 with
  8 partitions on Redis, including the parallel dimension reader's re-read.
- Subset Q9 under a lowered cap returns the same rows as the in-memory streamed join, with a
  bounded live heap compared with the eager fallback.
