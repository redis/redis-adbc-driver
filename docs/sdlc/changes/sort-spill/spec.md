# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Where it applies

`streamSelect` tries `sortSpillSelect` after `selectTopK` declines and before `selectScanned`.
`selectPlan.sortSpillable` admits a streamed plan when all of the following hold:
- there is a stream, without DISTINCT and without `stream_required`;
- the ORDER BY is not served by the index (`!indexSort`);
- there is no late materialization, and no `where.none` or key lookup;
- the scan is an ordinary index or directory scan: not a HASH or live scan, and no pending
  cleanup;
- there are no windows and no set-returning functions;
- no ORDER BY or select-list expression contains a subquery, window, aggregate or volatile
  function.

Retained partition captures (`retainedAllocationKey`) keep the materialized path.

## Buffering and runs

It runs the same `openScan` / `it.next` loop as `e.scan`. Each row's ORDER BY keys are
evaluated as it arrives, and the row is buffered with a scan sequence number.
`sortRowBytes` estimates retained bytes: 168-byte slots per eight-field map group, string and
decimal payloads, and keys.

- **Within the budget:** if the scan ends before the buffer reaches `sortSpillBytes` (256 MiB),
  the rows and their precomputed keys go to `selectRows`. That is `selectScanned` after its
  scan, unchanged, so small sorts behave exactly as before.
- **Over the budget:** when the buffer reaches it, the buffer is sorted by
  `(lessKeys(keys), seq)` and written as a run in a directory from `os.MkdirTemp`. A run record
  holds the sequence number and each field's name index, type index, NULL flag and
  `encodeStored` text.
- **Type and name tables:** these stay in memory and are never persisted. Reading a record
  decodes each field with `decodeStored`, then recomputes its keys with the same ORDER BY
  expressions. Eligibility guarantees they are deterministic and read only the row.

## Merging and streaming

- After the scan, `checkReads` validates the statement's metadata reads. The last buffer stays
  in memory as one more sorted source.
- Runs merge `spillFanIn` (32) at a time until one merge suffices.
- The result is a `streamReader` whose row source (`streamRowSource`, now shared with
  UNION ALL streams) is a k-way heap merge ordered by `(keys, seq)`. That equals the in-memory
  stable sort on scan order.
- The source applies OFFSET and LIMIT first, then evaluates the select list for each returned
  row only, then `fitRow` (and `Coerce` when batch bytes are tracked), as the scan stream does.
- Closing the stream (end of rows, early Release, statement or connection close) closes the
  files and removes the directory. An error before the stream is returned removes it too.

## Unchanged

Index sorts, top-K heaps for LIMITs, the materialized path for every other plan, and result
types. The UNION ALL stream's behavior is unchanged; only its field type became an
interface.

## Acceptance

`TestSQLSortSpill`, with a 16 KiB budget over 3,000 rows:
- six spilled queries return the in-memory sort's rows in the same order, covering ties, NULLs,
  NaN / ±Infinity, decimals, timestamps, DESC, `NULLS FIRST/LAST`, OFFSET / LIMIT, expressions
  and a residual;
- at least one sort merges in more than one pass;
- every run's buffer stays within the budget plus one row;
- directories are removed after a full read and after an early Release;
- skipped and limited rows don't evaluate the select list.

On the subset, a full `lineitem` sort validates its exact order against DuckDB with a lower
peak RSS.
