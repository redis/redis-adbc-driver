# Exact aggregate evidence (#406)

Full-scale acceptance is **complete**: all 24 paired native trials and eight
CPU/allocation profiles validate exactly on 99,997,497 rows. See the
[full-scale report](full/README.md) for pinned builds, raw data, cost tradeoffs,
verified S3 preservation and the 05:52Z window release. Full medians in seconds:
Q2 95.526→0.0636; Q4 80.600→78.149; Q9 159.663→96.586; Q10 280.192→113.826.
These cover four targeted ordinary-statement aggregates, not a product ranking.

Albert merged [PR #428](https://github.com/alberttwong/redis-adbc-driver/pull/428)
at 2026-10-22T02:03:11Z, closing #406. All ten checks passed on ad2faef. The
full comparison was completed afterward in the authorized restored-data window.
The sections below retain the earlier prefix and delayed-link evidence.

Baseline e692a704d2b70428086e145df00e73db7b1a253f; production b40b88f,
test follow-up c064caa; runner 14208bd. The immutable official prefix contains
100,000 rows and 105 columns. Parquet SHA256:
`d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`.
Local Redis 8.6.2/Search 8.6.0: four CPU, 2GiB Docker limit, macOS 26.7.1 arm64,
18 logical client CPU/36GiB RAM, loopback TCP without TLS. Native C-Arrow
timings use Go 1.27.2, four requested query workers and four budget tokens.
Q9/Q10 actually grant two workers for the two 65,536-row logical partitions.
Connection/setup is outside query elapsed time; execute and complete Arrow fetch
are included. Worker CPU includes setup. Five paired rounds alternate order;
all 40 results pass exact independent values/types/global rank/tie checks.
No build, test or profiling job ran during either canonical timing matrix.

[Raw prefix report](final/prefix.json), [exact oracle](final/prefix-oracle.json),
[build identities](final/provenance-final-mac.json). Ranges are all five trials.

| Query | Before median (range), seconds | After median (range), seconds | Before→after process CPU, seconds |
|---|---|---|---|
| Q2 | 0.230 (0.207–0.337) | 0.016 (0.014–0.027) | 0.329→0.012 |
| Q4 | 0.437 (0.384–0.481) | 0.424 (0.360–0.467) | 0.396→0.239 |
| Q9 | 1.099 (1.083–1.122) | 0.698 (0.682–0.755) | 0.136→0.302 |
| Q10 | 1.145 (1.137–1.173) | 0.663 (0.638–0.700) | 0.183→0.348 |

Q2's original issue was unsupported <>/!= recognition, not SMALLINT versus
INTEGER zero. Two positive exact numeric ranges exclude NULLs. Rounded/large
bounds and missing defaults keep rechecks. The new integer-only guard runs
before timezone planning; noninteger comparisons keep their existing fallback.
Q4 still issues roughly one HMGET per source row to read authoritative BIGINTs;
pipelining means command count is not round-trip count. Prebound typed inputs
remove row-map/expression dispatch while retaining checked integer accumulation.
Q4's ~3% elapsed difference has overlapping ranges; the clearer gains are CPU
and allocation. No lossless command-count reduction is claimed for Q4.

Q9/Q10 select bounded client complete-tuple compaction on this fast link. This
avoids Redis LOAD-all work over 105 fields; global exact DISTINCT/weighted sums
still use the existing external merge. The fixed 4096-entry map and existing
cursor/pipeline bounds remain in force, including the concurrent tail range.
The tradeoff is more HASH commands, network bytes and client allocation.

| Query | Before→after server CPU, seconds | Before→after INFO command delta | Before→after request+reply, MB | Before→after peak process RSS, MiB |
|---|---|---|---|---|
| Q2 | 0.139→0.002 | 33→14 | 13.31→0.02 | 145.6→82.3 |
| Q4 | 0.197→0.189 | 100,034→100,034 | 22.09→22.09 | 155.1→132.5 |
| Q9 | 1.052→0.209 | 22→100,051 | 1.96→24.33 | 106.9→103.6 |
| Q10 | 1.087→0.218 | 22→100,050 | 3.54→30.03 | 126.4→104.4 |

INFO deltas include monitor/control traffic. Peak primary memory is sampled
every 250ms; all raw values are retained. This is neither a hard Redis memory
bound nor a clean server/socket-wait wall-time decomposition. Prefix Q9 returns
7,256 complete tuples and writes/reads 537,374 spill bytes in both plans; Q10
returns 7,912 tuples and writes/reads 791,050 bytes in both. The full report now measures tuple and spill behavior; the 4096-entry map
flushes repeatedly on that fixture.

The [delayed-link matrix](final/delayed-link.json) has 24 additional exact results
over three paired rounds, using the existing userspace full-duplex model at
1Gb/s per connection/direction and 10ms configured added RTT. Calibrated median
PING is 13.34ms. This is not a shared-NIC bandwidth cap or a production network.
All six Q9/Q10 candidate executions retain server tuples. Median Q9 is
1.165→1.171s and Q10 is 1.204→1.259s: planner probe overhead is visible, especially
on Q10. Three steady-state PING samples per primary must all be below 2ms;
warm connection setup is excluded from samples. Failed/slow probes preserve the
server source. This conservative RTT heuristic does not estimate bandwidth or
guarantee a gain on every low-latency deployment.

[Profiles](profiles/) contain eight complete, independently checked executions
and raw CPU/allocation profiles. Profile CPU covers the Go test process including
connection/setup; logged MemStats differences cover execute and complete fetch.
Their Go Arrow allocator differs from native C Arrow in primary timings. The
measured query allocation volumes in MB are Q2 190.4→0.219, Q4 258.0→167.6, Q9
60.5→211.1 and Q10 107.2→246.5. GC counts are 11→0, 14→12, 12→33 and 14→38.
Cumulative allocation is not peak memory. The compact-tuple worker timer includes
local reduction and synchronous spill callbacks, overlapping spill phase timers;
these cumulative worker durations must not be summed into wall time. Planning,
expression and Arrow functions remain visible in raw profiles, but stages below
the sampling resolution cannot be assigned reliable CPU time. The Q9/Q10 source lowers server CPU
by about 80% while increasing client work; do not describe it as an allocation
reduction. Q2 is too brief for stable CPU flamegraph attribution.

[Validation](validation/manifest.json) retains full Go, native, race and focused
logs. Corrected production passes 1,075 Go test/subtest events with 17 skips,
zero failures. Native standalone RESP3/RESP2 and Cluster RESP3 each pass
376 tests with eight skips and three subtests. Focused races and strengthened
cross-partition/NULL/overflow/empty/cancellation cases pass on standalone and
both Cluster protocols. The full Go skip list and pre-existing untracked
diagnostic caveat are in the [SDLC record](../../../docs/sdlc/changes/aggregate-performance-406/validation.json).

Three-pass agent [review](../../../docs/sdlc/changes/aggregate-performance-406/review.md)
reproduced a non-UTC TIMESTAMP <> TIMESTAMPTZ regression; b40b88f fixes it by
moving the integer-only guard before localBounds. Both operand orders now return
both matching rows. Missing-default tuple inputs retain the existing fallback.
The former full-scale evidence gap is now resolved by the linked full report.
The maintainer merge, passing CI and full validation are recorded in the SDLC record.

[Exploratory screens](preliminary/) retain 96 earlier exact executions. c7038b2
first reduced map/parser work; 8310e07 added typed vectors. A mixed Search/HASH
compactor had no elapsed gain and was superseded by complete HASH slices. Its
two uncommitted prototype reports were supplied the preceding vector provenance
JSON by mistake. Original reports remain unchanged; adjacent provenance files
record the correction and actual candidate binary hash. Attribute neither
prototype to committed 8310e07. Canonical pinned results above supersede them.

[Matched Linux library identities](final/provenance-final-linux.json) were used in the full-scale window, using Go 1.26.6, identical flags/compiler
and four-CPU/8GiB local cross-build containers. Full comparisons used sixteen
internal workers and a 16GiB per-trial memory cap on the dedicated 16-vCPU/32GiB client, preserving historical results and the
fixed resource expiry. The later retained-partition changes are outside the
pinned measured binaries.

The [reserved-window operations](full-window/operations.md) preserve the exact
launcher and optional profile scripts. Setup and every isolated trial are
journaled before launch. Twelve harness tests pass (eight isolation, four exact
comparison/gate tests); Linux profile helpers launch successfully. The owner
released the window only after backup/restore verification; all 34 owned services were inactive before the verified evidence handoff back to the owner.
