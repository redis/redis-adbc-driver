# Native missing-row proof and point attribution

Date: 2026-11-14. Baseline: `b904cf8d`; candidate runtime changes: `go/snapshot_delta.go` and `go/snapshot_missing_row.go`. [Source/binary provenance](provenance.json), [checks](checks.json), [design](../../docs/validated-point-protocol.md), [SDLC](../../docs/sdlc/changes/validated-point-protocol/intent.md).

The candidate batches existing native ZCARD and ZSCORE reads for each missing published image. It keeps the same command count and saves one dependent client round trip per segment miss. It also fixes a proven safety bug: wrapped/joined Redis Nil previously counted as absence and exposed an older image. Four RED cases reproduced that behavior on baseline; candidate rejects all four before any older HASH read. Typed NoRetry markers prevent transparent proof replay, including helper calls outside execution transport. Private-owned segments retain scalar score-only verification; found rows and strict membership paths are unchanged.

**No general latency improvement is claimed.** This is partial Go SQL attribution with one client and a pre-acquired execution transport, not the public Python/ADBC benchmark. `statisticsHookedTransport` creates/attaches one execution transport for the entire run. Per-statement execution-pool acquisition, transport owner registration, and finish/return/drain are outside each timer. Query creation/planning/fetch/conversion, pin lifecycle and statement close still run. Historical emitted JSON labels are preserved verbatim with an explicit post-run scope correction field; final source emits a corrected label. Both binaries were built and frozen before all timings; A1/B1/B2/A2 ran consecutively without intervening source changes, builds or sibling heavy work. Every run measured all seven cases, 16 samples after four warmups: 448 measured queries and 112 warmups total. All results were fully fetched and their Arrow INT64/STRING types, int64 extrema/values beyond 2^53, NULL, empty strings, absence and overlay visibility were validated.

## Complete timing and request counts

Times below are per-round p50 milliseconds. Full observations, nearest-rank p95, raw control timings, command totals, ordered standalone requests and duration per pipeline shape are retained in the four JSON/CSV pairs.

| Case | RTT baseline → candidate | Native commands, both | A1 | B1 | B2 | A2 |
|---|---:|---:|---:|---:|---:|---:|
| single_segment_hit | 11 → 11 | 20 | 2.341 | 2.383 | 2.406 | 2.370 |
| single_segment_null | 11 → 11 | 20 | 1.977 | 2.037 | 2.031 | 1.960 |
| single_segment_absent | 13 → 12 | 22 | 2.277 | 2.298 | 2.102 | 2.308 |
| overlay_newest | 11 → 11 | 24 | 16.485 | 16.140 | 15.868 | 17.142 |
| overlay_tombstone | 14 → 13 | 27 | 16.439 | 16.972 | 16.123 | 15.563 |
| overlay_oldest | 17 → 15 | 30 | 16.984 | 16.352 | 16.078 | 16.101 |
| overlay_absent | 19 → 16 | 32 | 17.087 | 16.623 | 16.070 | 16.165 |

Point-hit medians and some miss/tombstone observations are mixed. In B1 the single-segment miss p95 was 4.592 ms, versus 2.449/2.572 ms in A1/A2; B2 was 2.281 ms. With only 16 samples per case, nearest-rank p95 is the maximum. These observations and drifting controls do not support a causal overall p50/p95 speedup claim. The deterministic request reduction and fail-closed correction are the supported conclusions.

The ordered trace isolates the large overlay attribution to the **first standalone `control_valid` request**: median approximately 11.2–13.8 ms across overlay cases/rounds, while the following two requests remain approximately 0.23–0.29 ms. The native valid branch uses point reads, not catalog enumeration. Client-observed hook time includes pool/scheduler/transport/decode and does not prove server CPU or a Lua regression. Isolated control requests and server-duration evidence are still needed.

## Independent physical-key control

Each SQL observation is followed by a redis-go HGETALL on an independent application HASH, through a separate pool at the same endpoint. The control independently verifies exact int64/empty values. It uses a known physical key and performs no logical segment resolution, managed pin/ownership, NULL-schema or tombstone validation; it is not semantically equivalent to the SQL scenarios and is not redis-py. Fixed ordering supplies a transport diagnostic, not a causal A/B comparison. No control-normalized ratio is claimed.

| Case | A1 control p50 | B1 control p50 | B2 control p50 | A2 control p50 |
|---|---:|---:|---:|---:|
| single_segment_hit | 0.191 | 0.187 | 0.212 | 0.195 |
| single_segment_null | 0.157 | 0.175 | 0.166 | 0.155 |
| single_segment_absent | 0.161 | 0.166 | 0.160 | 0.156 |
| overlay_newest | 0.272 | 0.275 | 0.251 | 0.249 |
| overlay_tombstone | 0.253 | 0.276 | 0.270 | 0.272 |
| overlay_oldest | 0.248 | 0.242 | 0.249 | 0.241 |
| overlay_absent | 0.246 | 0.261 | 0.256 | 0.237 |

## Environment, checks and limits

The coordinator verified active/effective 4x QPF, six Search workers, mnp/32 connections, 4 GiB/noeviction, strict certificate/hostname TLS and RESP3 on exclusively owned Redis Software before each process. Go 1.27.3, GOMAXPROCS 4, telemetry disabled. Production maintenance remained enabled on an independent unhooked pool. Public provenance contains configuration/source/binary hashes and generic host platform, with operational identifiers/credentials omitted. Server build identifiers were not included in the sanitized coordinator observation; no specific server release is asserted here.

Candidate focused proof/privacy/descriptor/lease regressions: 40 PASS events including subtests, zero failures/skips. Focused Software corruption/visibility/strict-path checks: 27 PASS events including subtests, zero failures/skips. Each full attribution run passed all result/type oracles; all process/client/worker ownership was cleaned before releasing the fixture. Baseline RED intentionally has four failing leaf cases plus their failing parent. This is not the full unit/race/cluster suite and provides no four-client or public native benchmark claim.

The initial pre-experiment observation is retained as `initial-profile.json` / `initial-summary.csv`. Its setup put the updated row one segment below the newest tombstone, and it lacked the final ordered/per-shape duration and explicit Arrow type-ID assertions. Keep its request counts/timings historical; do not merge them into the final ABBA or relabel its cases as the final physical layout.

Reproduce the summaries without Redis:

```sh
python3 benchmarks/validated-point-protocol/summarize.py benchmarks/validated-point-protocol/A1.json
```

To rerun the workload, use an exclusively owned disposable Software database after the project QPF/noeviction verification, set `REDIS_SNAPSHOT_URI` with strict TLS trust and `REDIS_POINT_PROTOCOL_PROFILE=1`, then run `go test -v -count=1 -p 1 -run ^TestSnapshotPointProtocolProfile$ .` from `go/`. The fixture clears its own database and Search definitions. Do not run against an application database. No ClickBench, external infrastructure or upstream issue/comment was created.
