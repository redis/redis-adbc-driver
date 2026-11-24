# Validation

2026-11-24; branch merged with main `8bb53c65`. See [intent](intent.md), [spec](spec.md),
[plan](plan.md).

## Regression

`go/snapshot_candidate_windows_integration_test.go` drives `newSnapshotCandidateScan` with a
four-row page (64-row windows) over 600 rows with dense, sparse and empty regions, an UPDATE that
stops one match, an UPDATE that starts another and a DELETE (four segments).

- Original `snapshot_candidates.go`: fails with 0 bounded windows and 63 open-ended pages for
  4 sources (results were already complete; the defect is cost, not rows).
- Fixed: `windows`, `selective` (one Search query per source) and `stale bound` (complete via
  the open-ended tail) pass. Candidate-path prefetch tests
  (`TestSnapshotPrefetchUpdateDeleteAfterDiscovery/candidates_true`, `TestPrefetch*`) pass.
- Unit-only pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`, exit 0.

## Root-cause measurements

Local Redis Software 8.8.0 fixtures, one shard, verified active 4× QPF, `mnp`, 32 connections,
`noeviction`, TLS. Numbers before the main merge (`c27bb970`) are labeled stale and kept only to
show the mechanism; sibling sessions shared the subset server, so absolute times are noisy.

- Scanner SQL (scratch-only trace build, not committed): Q19 through `adbc_scanner` `98ff0a39fc`
  sends `SELECT "l_partkey", "l_quantity", "l_shipmode", "l_shipinstruct", "l_extendedprice",
  "l_discount" FROM "public"."lineitem" WHERE "l_shipinstruct" = ?` and an unfiltered projected
  `part` scan. The optional `l_shipmode IN` hint is not sent. Bound parameters push the same
  Search query as literals.
- Single-page `FT.SEARCH … SORTBY __rowid LIMIT 0 4096` latency on the 600,093-row subset index
  (stale): open-ended `@__rowid:[(after +inf]` pages cost 30–91 ms and fall as `after` advances;
  65,536-row windows cost 10–27 ms at every position, for `*`, TAG equality, TAG union and
  numeric range filters.
- SF1 (6,001,215-row `lineitem`, index complete: `indexing=0`, `percent_indexed=1`), dedicated
  one-shard Software RAM fixture with a 14 GiB database, macOS host client through the published
  port: an open-ended first page took 11.7 s and an open-ended page after row 1,500,303 took
  18.6 s; a 65,536-row window took 0.43 s. The scanner's Q19 `lineitem` read needs about 366
  pages of 4,096 rows. After these pages Search refused further queries with "Coordinator failed
  to execute the query due to insufficient memory" (12.2 GB used of 15.0 GB): the 14 GiB profile
  is too small for SF1 queries, so the full SF1 A/B did not complete and the fixture was removed
  to free shared Docker memory. The issue's profile used 18 GiB.

## Subset before/after (main `c27bb970`, stale)

Same 10% TPC-H subset (600,093 `lineitem`, full `part`), shared one-shard Software RAM fixture
(8 GiB, 4× QPF), macOS host client via published port, base and fixed builds interleaved; the
fixed build here is the first window design (open-ended first page, fixed 16-page windows).
Scanner-shaped `lineitem` reads: TAG equality (150,019 rows) 2.38 → 1.61 s; numeric range
(600,093 rows) 14.56 → 6.77 s; TAG union with residual (299,912 rows) 68.54 → 22.85 s.
Third-path Q19 (DuckDB 2.0.1, released `adbc_scanner` `98ff0a39fc`): 5.34 / 3.93 s →
3.25 / 3.18 s, all returning the DuckDB reference revenue `277320.2089`.

## Pending

- Full SF1 third-path Q19 (and #627) A/B on a full-memory (18 GiB) Software RAM fixture with a
  Linux client in the server namespace, after the parent's Flex verification frees memory.
- The 256-row candidate/directory page used when a residual is present remains (out of scope).
