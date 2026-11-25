# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`newSnapshotCandidateScan` marks a scan `completeUnion` when its page is positive and its
pushed query has at least `snapshotUnionCompleteTerms` (64) union alternatives, counted as
`" | "` separators (`unionAlternatives`). Numeric unions `(@c:[a a] | @c:[b b])` and TAG
unions `@c:{a | b}` use that separator; escaped TAG text cannot contain it, because spaces
are escaped. The count only selects how candidates are read.

On the first `head` of each source that is not owned (private) or done, a `completeUnion`
scan calls `readCompleteUnion` once:

- `FT.SEARCH index query NOCONTENT LIMIT 0 page TIMEOUT 0 DIALECT 2`, unsorted and without
  row-ID bounds.
- If `total_results` exceeds the page but not the cap, one more read with `LIMIT 0 total`.
  The cap is the smallest of the Search result cap (`search-max-search-results`),
  `snapshotUnionCandidateCap` (131,072, the Flex count cap) and 16 MiB of keys at
  `len(prefix) + 20` bytes per key.
- A partial-result warning is an IO error, as for windows. Every key must carry the
  source's prefix and a canonical decimal row ID in `[1, 2^53)`. Duplicates are an IO error.
- The source then holds its sorted, zero-padded IDs and is done.
- If more candidates match than the cap, the second read still finds more than its limit,
  or the key count differs from `total_results`, the source is left unchanged and uses
  row-ID windows exactly as before.

Unchanged: the merge across sources, newer-directory visibility (ZMSCORE), the
shadowed-HASH identity checks, tombstones, row fetches, the final directory verification
(ZCARD), page sizes, and the windowed path for every query below the threshold. Flex scans
never reach this path (`snapshotIndexable` is false under Flex).

Acceptance: `TestSnapshotCandidateBoundedWindows/large_union` fails before the change
(66 evaluations of a 202-term union for 2 sources) and passes after (at most two per
source), returning the complete ordered rows with an updated and a deleted key. Existing
candidate, union, join and semi-join tests pass. On the 10% TPC-H subset, the #631 lookup
shape (`l_orderkey IN (1,000 keys)`) and the union-using TPC-H queries are validated
against DuckDB and are not slower.
