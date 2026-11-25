# Validation

2026-11-25; base `ed196803` (main). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database (`software-setup.sh`, `SOFTWARE_SHARDS=1`).
Client: macOS ARM64 host (10 CPUs, Go 1.27.4) through a Docker published port; other sessions'
fixtures shared the Docker VM. Fixture: the 10% TPC-H subset (orders with `o_orderkey % 10 = 1`
plus their lineitems, full dimension tables; 1.91M rows) loaded through the driver.

## Q9 profile

A scratch `go test -overlay` runner (not committed) executed subset Q9 three times in one process
on the fixture, reading the full Arrow stream, with `-cpuprofile` and `-memprofile`
(`-memprofilerate 65536`). Allocation is `runtime.MemStats.TotalAlloc` / `Mallocs` per execution. The main build is `2ed3b3df`,
whose driver code is identical to `ed196803` (#676 changed tests only); the A/B uses the same build.
"GC CPU" is the profile's cumulative samples under `gcBgMarkWorker`, `gcAssistAlloc`, `bgsweep`
and `bgscavenge`.

| | main `2ed3b3df` | this change |
|---|---:|---:|
| allocated per run | 6.00 / 5.99 / 5.99 GB | 4.91 / 4.91 / 4.91 GB (−18%) |
| mallocs per run | 79.9M | 63.1M (−21%) |
| GC CPU / profile total | 11.83 s / 36.82 s (32.1%) | 10.14 s / 34.01 s (29.8%) |
| run time | 11.19 / 9.12 / 7.79 s | 11.66 / 9.46 / 9.57 s |

Allocation sites (three runs, `alloc_space`) before → after:

| site | main | this change |
|---|---:|---:|
| `decodeRowCapacity` | 6,401 MB | 6,403 MB |
| `compactDimensionRow` | 1,976 MB | gone (in place) |
| `fetchRowsChunk` | 1,733 MB | 1,742 MB |
| `nextDirectoryKeys` (+ `snapshotKeyPair`) | 1,474 MB | 368 + 554 MB |
| `parseDecimal` / `rescaleDecimal` (`big.Int.SetString`, same-scale copy) | 310 + 249 MB (614 + 323 cumulative) | `parseSmallDecimal` 239 MB |

Run time on this route is dominated by Redis round trips and stayed within noise.

## Tests

- `TestDecimalRowAllocations` (new) with main's `value.go` and `eval.go` (via `-overlay`) fails:
  `decodeStored DECIMAL(15,2): 6 allocations, want at most 2` and `decimal +: 5 allocations,
  want at most 2`. With the change it passes, and the results and operands are unchanged
  (including `a - a`, an integer operand and a stored value at a smaller scale).
- `TestParseSmallDecimalMatchesTextPath` (new) checks the original text parser's value, scale
  and validity on 35 edge cases and 2,000 random inputs: signs, leading zeros, `.5` / `5.`,
  exponents, 18 / 19 digits, `int64` extremes, and invalid text including non-ASCII digits.
- `TestCompactDimensionRowOwnership` (new):
  - A fresh row of at most eight entries is compacted in place.
  - A shared row, or one of more than eight entries, is copied and left unchanged.
  - `freshDimensionRows` accepts managed tables and in-memory relations, and rejects views,
    materialized SQL and adopted tables.
- `TestSnapshotKeyPairShares` (new) shows one string for both keys at the table's prefix and
  two at another segment's.
- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. `gofmt -l` on the
  changed files: clean.
- Integration selection `-run 'Snowflake|Star|Decimal|Numeric|Exact|Join|Aggregate|Candidate|Delta|Directory'`
  (273 top-level tests, including `TestSQLSnowflakeJoinsManaged`, which compares snowflake and
  eager results on Redis) through `software-setup.sh --exec`: `ok … 361.958s`. Snapshot suites
  that need `REDIS_SNAPSHOT_URI` skip as usual.

## TPC-H subset A/B

Each query in two fresh processes per library, two in-process repetitions each, libraries
alternating, fully fetched and validated against DuckDB 2.0.1 (row multiset, rel 1e-7 /
abs 1e-6). **All 88 runs validated.** From about the 20th run on, the integration selection ran
on the same fixture, so times carry that load; allocation per execution does not depend on it.

| query | main median (min–max) | this change median (min–max) | allocated per run, main → change |
|---|---:|---:|---:|
| Q1 | 4.11 s (3.96–12.66) | 4.30 s (3.04–20.92) | 6.65 → 6.48 GB |
| Q3 | 5.95 s (4.70–11.22) | 4.36 s (4.22–9.27) | 2.44 → 2.22 GB |
| Q5 | 4.01 s (3.74–4.13) | 4.35 s (3.35–6.70) | 2.53 → 2.25 GB |
| Q6 | 0.35 s (0.27–0.80) | 0.37 s (0.22–0.71) | 0.04 → 0.04 GB |
| Q7 | 2.34 s (1.71–10.41) | 1.98 s (1.57–5.51) | 0.58 → 0.47 GB |
| Q8 | 3.92 s (3.56–4.25) | 4.23 s (3.52–7.66) | 2.94 → 2.71 GB |
| Q9 | 13.45 s (11.90–16.81) | 11.55 s (8.40–13.38) | 6.00 → 4.91 GB |
| Q10 | 4.37 s (3.40–5.26) | 3.68 s (3.36–3.98) | 2.40 → 2.21 GB |
| Q14 | 0.86 s (0.81–1.15) | 0.83 s (0.81–0.88) | 0.58 → 0.45 GB |
| Q18 | 3.34 s (2.79–4.47) | 3.20 s (3.14–3.53) | 3.99 → 3.82 GB |
| Q19 | 0.62 s (0.54–0.73) | 0.65 s (0.55–0.69) | 0.12 → 0.12 GB |

Allocation is a process-wide figure from execution diagnostics, as the issue specifies. It fell
in every query that joins or does decimal arithmetic; times are treated as unchanged.

## Not established

- The remaining cost is the row representation: `decodeRowCapacity`'s per-row maps, 46% of Q9's
  bytes, and the raw reply maps, 12%. A Go map with at most eight entries is one 8-slot group,
  about 1.4 KB with the 152-byte `Value`, whatever the column count. Reducing it needs
  column-typed or positional rows or a smaller `Value`; tracked as a follow-up.
- SF1 latency; SF1 cannot be hosted on this machine (#648). On SF1 Flex, readers wait on replies
  87–90% of the time, so the latency gain there may be small.
- Multi-shard behavior was not measured locally (CI runs three shards).
- Human review is pending.
