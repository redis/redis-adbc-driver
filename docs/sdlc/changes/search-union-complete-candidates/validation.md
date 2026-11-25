# Validation

2026-11-25; base `268ccf03` (main). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database, set up by `go/software-setup.sh`
(`SOFTWARE_SHARDS=1`). Client: macOS ARM64 host (10 CPUs) through a Docker published port, so
absolute times carry that route's caveat. Two other sessions' Software fixtures ran on the same
Docker VM. Timings are relative evidence, not the SF1 Linux-namespace profile.

Fixture: the 10% TPC-H subset used by earlier records (every order with `o_orderkey % 10 = 1`
plus its lineitems, full dimension tables; 1,910,123 rows), generated with DuckDB 2.0.1's
`dbgen(sf=1)` and loaded through the driver from main `268ccf03`; every table's count matched.

## Cause (raw Search)

`FT.SEARCH` on the `lineitem` index, `@l_orderkey` unions of k distinct real order keys, best of
three:

| terms | matches | `LIMIT 0 0` | sorted page of 1,000 | one row-ID window `(0 100000]` | unsorted `LIMIT 0 10000` |
|---:|---:|---:|---:|---:|---:|
| 100 | 379 | 13.7 ms | 21.5 ms | 6.3 ms | 9.4 ms |
| 256 | 992 | 14.8 ms | 20.7 ms | 9.3 ms | 25.0 ms |
| 1,000 | 3,960 | 49.7 ms | 61.2 ms | 36.6 ms | 67.8 ms |
| 4,000 | 16,035 | 192.0 ms | 214.4 ms | 129.2 ms | 234.4 ms |
| range `l_orderkey < 400000` | 40,146 | 2.8 ms | 21.1 ms | – | – |

A union costs about 40–50 µs per term per evaluation, roughly linearly, even when a row-ID
window holds a sixth of the rows. Four 250-key unions issued one after another took 117.9 ms
against 67.8 ms for one 1,000-key union, so splitting a union into bounded batches would not
help. The cost is the union iterator's per-child setup, as expected; no upstream issue is filed.
The driver's candidate scan re-evaluated the union in every window, about twenty for 4,000 rows.

## Tests

- `TestSnapshotCandidateBoundedWindows/large_union` (new). On unmodified `snapshot_candidates.go`
  it fails: `66 Search evaluations of a 202-term union for 2 sources`. With the change it passes
  (at most two per source), returning the complete ordered rows across two segments, including a
  key with a newer version (50, 250) and a deleted key (60). The other subtests (`windows`,
  `selective`, `stale bound`) pass unchanged.
- Integration selection `-run 'Snapshot|Candidate|Union|Join|SemiJoin|Subquer|InList|RowValue|Star|Snowflake|Q19|Common|FactDemand|Deferred|Lateral|Natural|Prefetch|Exists'`
  through `software-setup.sh --exec` on the fixture above (294.7 s): all pass except
  `TestSnapshotCopyChecklistPrivateNames`, which failed with `it_hc_private_names_source is still
  indexing`. That is #653: its application index's initial scan covers the 1.94M subset keys
  (70 s measured) against a fixed 10 s wait. It does not read snapshot candidates.
- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. `gofmt -l` on the
  changed files: clean.

## The #631 lookup shape through the driver

`SELECT l_orderkey FROM lineitem WHERE l_orderkey IN (<k keys>)`, one process per library,
three repetitions each, fully fetched (row counts identical between builds):

| shape | rows | main `268ccf03` | this change |
|---|---:|---|---|
| 100 keys | 412 | 111 / 72 / 64 ms | 44 / 51 / 54 ms |
| 250 keys | 1,011 | 161 / 153 / 147 ms | 77 / 73 / 73 ms |
| 500 keys | 1,987 | 301 / 300 / 310 ms | 125 / 107 / 135 ms |
| 1,000 keys | 4,001 | 709 / 670 / 656 ms | 203 / 206 / 203 ms |
| range `< 400000` | 40,146 | 437 / 272 / 237 ms | 259 / 252 / 235 ms |

Command counts were unchanged apart from the removed window queries (1,000 keys: 4,051 →
4,030). The 1,000-key lookup is now 3.3× faster and costs about what the range costs for ten
times as many rows; the issue's 8–9× gap was the window re-evaluation.

## TPC-H subset A/B

Each query in a fresh process, two rounds of two repetitions per library, alternating, fully
fetched and validated against DuckDB 2.0.1 on the same subset (row multiset, rel 1e-7 /
abs 1e-6). All 64 runs validated.

| Query | plan | main (s) | this change (s) | medians |
|---|---|---|---|---|
| Q3 | parallel streaming star | 4.60 / 3.78 / 3.91 / 3.45 | 3.68 / 3.75 / 3.69 / 3.94 | 3.84 → 3.72 |
| Q4 | select scan + semi-join probes | 1.76 / 0.54 / 0.68 / 0.71 | 0.34 / 0.29 / 0.35 / 0.32 | 0.70 → 0.33 |
| Q5 | parallel streaming star | 4.17 / 3.97 / 3.33 / 3.29 | 3.31 / 3.33 / 3.67 / 3.76 | 3.65 → 3.50 |
| Q7 | streaming snowflake | 1.71 / 4.32 / 1.43 / 1.56 | 1.50 / 1.30 / 1.62 / 1.20 | 1.64 → 1.40 |
| Q8 | parallel streaming star | 3.65 / 3.23 / 3.19 / 3.26 | 3.40 / 3.41 / 3.05 / 3.17 | 3.25 → 3.29 |
| Q18 | streaming snowflake | 2.92 / 2.49 / 3.15 / 3.63 | 2.69 / 2.77 / 2.98 / 3.63 | 3.04 → 2.88 |
| Q19 | eager join (`lookup_join_order`) | 0.82 / 0.46 / 0.57 / 0.48 | 0.57 / 0.43 / 0.56 / 0.53 | 0.53 → 0.54 |
| Q21 | select scan + semi-join probes | 3.56 / 3.29 / 2.41 / 2.70 | 2.63 / 1.70 / 2.40 / 2.60 | 2.99 → 2.50 |

Q4's 256-key semi-join probes each read their candidates in one evaluation instead of several
windows. The star-join queries do not read large unions through the candidate scan and are
within noise.

## Not established

- SF1 latency on the issue profile (Linux client in the server's network namespace); SF1 cannot
  be hosted on this machine. #648 tracks the SF1 rerun.
- Multi-shard behavior was not measured locally; CI runs the Software suite on three shards.
- Human review is pending.
