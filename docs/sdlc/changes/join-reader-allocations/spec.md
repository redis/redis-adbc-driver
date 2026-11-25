# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

1. **Dimension rows compacted in place.** `streamJoinPages` marks a plain (non-branch)
   dimension `fresh` when `freshDimensionRows(base)` holds. That means a plain managed table
   (`managedRowIDTable`) or an in-memory relation read through `scanItem`'s per-row copies, and
   no view, lazy, grouped, joined, materialized-SQL or materialized-view read.
   - A fresh row with at most `smallRowEntries` (8) entries has its non-output columns deleted
     in place.
   - Every other row is copied as before.

   Decoders create dimension rows with one slot per decoded column (`decodedRowCapacity`,
   `ownedFactRowCapacity`) or grow them from empty. A row of at most eight entries therefore
   occupies one map group, the same as its compacted copy. Retained memory is unchanged, and
   the copy is saved. The rows kept and their columns are unchanged.
2. **One key string per row.** `snapshotKeyPair` returns the same string for a row's logical
   and physical keys when the source's prefix is the table's (its newest segment). It is used in
   both the exact-directory scan (`snapshotDeltaScan.nextKeys`) and the RAM candidate scan.
3. **Stored decimals.** `parseDecimal` parses at most 18 digits with `parseSmallDecimal`, which
   accumulates an `int64` and uses `SetInt64`. It skips the digit concatenation and
   `big.Int.SetString`. Anything else, including invalid text, takes the original path, so values,
   scales and errors are unchanged. `decodeStored` no longer copies a freshly parsed value that is
   already at the column's scale.
4. **Decimal `+` / `-`.** `binaryOp` reads operands that are already at the result scale instead
   of copying them, and writes the result to a fresh `big.Int`. Operands are never mutated.

Acceptance:
- `TestDecimalRowAllocations` fails on main (6 allocations per stored-decimal decode, 5 per
  decimal `+`) and passes with at most 2 each.
- `TestParseSmallDecimalMatchesTextPath` matches the original parser on edge cases and 2,000
  random inputs. `TestCompactDimensionRowOwnership` and `TestSnapshotKeyPairShares` cover
  ownership and sharing.
- Subset Q9 allocates fewer bytes per run with a lower GC share. TPC-H queries validate
  against DuckDB, and the snowflake/star and decimal integration selections pass.
