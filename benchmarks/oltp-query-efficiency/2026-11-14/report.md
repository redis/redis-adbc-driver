# OLTP and selective-query performance — November 14, 2026

This compares the merged November 13 driver with the next unreleased performance candidate. Both complete native matrices validated **168 records** each: 42 warmups and 126 measured trials, with no failed results, serialization conflicts, retries or discarded native records. The pair retains 5,472 measured operation observations. Each point case has 96 measured observations/build; each query case has 24.

## Changes and observed tradeoffs

- Queue existing catalog/pin proofs together and queue the authorized native descriptor GET with its presence proofs. No lease, permission or live-value cache; protocol 17 and original failure precedence remain.
- Fuse single-known-key managed writer-scope row reads and conditional absence/directory checks in a read-only script. Newest-winner/tombstone ordering, native strings, outer ownership/lease proofs and strict/multi-key paths remain. This targets UPDATE/DELETE image retrieval, not index-barrier semantics.
- Filtered unmanaged scans read exact selected HASH fields atomically with TYPE/HMGET instead of full HGETALL. HSTRLEN checks bound selected values to 1 MiB before Lua materialization; larger rows use the original native HGETALL path without truncation. Native command permission refusals retain compatibility; unknown/composed replies never trigger repair or fallback. Application HASHes remain live and externally owned. Unfiltered full-LOAD grouped retrieval is unchanged.

Single-client managed UPDATE improves **68.390→46.276 ms (1.48×)** and DELETE **57.002→40.519 ms (1.41×)**. Managed/unmanaged known-key reads improve **3.307→2.883 ms / 3.367→2.952 ms**, about 13–14% less elapsed time. Unmanaged filtered projection improves **16.938→5.115 ms (3.31×)** with one client and **21.135→11.654 ms (1.81×)** with four. Single-client unmanaged filtered COUNT/SUM improves **262.126→109.571 ms (2.39×)**.

**Material slower cases remain:** four-client unmanaged filtered COUNT/SUM increases **128.214→177.077 ms (+38.1%)**; four-client managed INSERT increases **45.868→53.152 ms (+15.9%)**. Four-client managed filtered COUNT/SUM increases 11.742→12.180 ms (+3.7%). Four-client managed DELETE is essentially flat, and single-client unmanaged grouped COUNT/SUM is flat/slightly slower (397.343→398.706 ms). These are retained tradeoffs, not excluded samples. No confidence interval or causal attribution follows from three finite trials. Script work/serialization, scheduling, cache state and allocator effects require separate attribution before claiming a cause. The managed image specialization does not apply to INSERT, so these observations alone do not attribute that regression to the specialization.

The driver remains much slower than redis-py for these OLTP operations: candidate point reads are roughly **12×** native command time, managed mutations **131–188×**, and unmanaged mutations **16–20×** across these cases. Managed snapshot publication supplies guarantees beyond direct native mutations; the comparison measures the complete requested operation rather than equalizing guarantees. Managed grouped aggregation remains faster on this synthetic fixture, but modes use different index/layout/execution strategies. This is not a universal product ranking.

## Complete comparison

Point fixture: 1,000 rows; query fixture: 100,000 rows. Both use four public int64/string columns plus an ordinal control field. Filter projection returns 100 rows, filtered COUNT/SUM matches 10,000, grouped COUNT/SUM returns 16 groups. Aggregate outputs are cast to BIGINT. Managed addressing uses known internal row IDs; unmanaged/raw addressing uses known application keys, with lookup outside timing.

Pooled p50 milliseconds below include execute through complete typed Arrow fetch or acknowledged mutation. Persistent application clients, setup/teardown and independent validation are outside timing; internal driver setup is included. This is closed-loop finite-work latency, not saturated capacity. Below 1× baseline/candidate means slower; below 1× candidate/redis-py means faster than the client baseline.

| Operation | Mode | Clients | Baseline ms | Candidate ms | Baseline/candidate | Candidate/redis-py |
|---|---|---:|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 4.667 | 4.269 | 1.09× | 1.41× |
| filtered_count_sum | managed | 4 | 11.742 | 12.180 | 0.96× | 1.33× |
| filtered_count_sum | redis_py | 1 | 3.003 | 3.025 | 0.99× | 1.00× |
| filtered_count_sum | redis_py | 4 | 10.633 | 9.177 | 1.16× | 1.00× |
| filtered_count_sum | unmanaged | 1 | 262.126 | 109.571 | 2.39× | 36.22× |
| filtered_count_sum | unmanaged | 4 | 128.214 | 177.077 | 0.72× | 19.30× |
| filtered_projection | managed | 1 | 19.021 | 17.008 | 1.12× | 15.06× |
| filtered_projection | managed | 4 | 24.797 | 21.928 | 1.13× | 4.59× |
| filtered_projection | redis_py | 1 | 1.095 | 1.130 | 0.97× | 1.00× |
| filtered_projection | redis_py | 4 | 4.623 | 4.776 | 0.97× | 1.00× |
| filtered_projection | unmanaged | 1 | 16.938 | 5.115 | 3.31× | 4.53× |
| filtered_projection | unmanaged | 4 | 21.135 | 11.654 | 1.81× | 2.44× |
| grouped_count_sum | managed | 1 | 12.545 | 12.109 | 1.04× | 0.35× |
| grouped_count_sum | managed | 4 | 20.104 | 19.592 | 1.03× | 0.19× |
| grouped_count_sum | redis_py | 1 | 34.734 | 34.660 | 1.00× | 1.00× |
| grouped_count_sum | redis_py | 4 | 105.802 | 105.834 | 1.00× | 1.00× |
| grouped_count_sum | unmanaged | 1 | 397.343 | 398.706 | 1.00× | 11.50× |
| grouped_count_sum | unmanaged | 4 | 473.114 | 440.357 | 1.07× | 4.16× |
| delete | managed | 1 | 57.002 | 40.519 | 1.41× | 170.77× |
| delete | managed | 4 | 57.749 | 57.393 | 1.01× | 138.34× |
| delete | redis_py | 1 | 0.244 | 0.237 | 1.03× | 1.00× |
| delete | redis_py | 4 | 0.412 | 0.415 | 0.99× | 1.00× |
| delete | unmanaged | 1 | 4.573 | 4.021 | 1.14× | 16.95× |
| delete | unmanaged | 4 | 8.857 | 7.978 | 1.11× | 19.23× |
| insert | managed | 1 | 44.590 | 42.872 | 1.04× | 171.19× |
| insert | managed | 4 | 45.868 | 53.152 | 0.86× | 130.61× |
| insert | redis_py | 1 | 0.271 | 0.250 | 1.08× | 1.00× |
| insert | redis_py | 4 | 0.418 | 0.407 | 1.03× | 1.00× |
| insert | unmanaged | 1 | 4.872 | 4.030 | 1.21× | 16.09× |
| insert | unmanaged | 4 | 8.573 | 8.320 | 1.03× | 20.44× |
| point_read | managed | 1 | 3.307 | 2.883 | 1.15× | 11.91× |
| point_read | managed | 4 | 6.591 | 5.779 | 1.14× | 12.24× |
| point_read | redis_py | 1 | 0.249 | 0.242 | 1.03× | 1.00× |
| point_read | redis_py | 4 | 0.451 | 0.472 | 0.96× | 1.00× |
| point_read | unmanaged | 1 | 3.367 | 2.952 | 1.14× | 12.20× |
| point_read | unmanaged | 4 | 6.282 | 5.594 | 1.12× | 11.84× |
| update | managed | 1 | 68.390 | 46.276 | 1.48× | 187.76× |
| update | managed | 4 | 92.015 | 69.605 | 1.32× | 165.90× |
| update | redis_py | 1 | 0.247 | 0.246 | 1.00× | 1.00× |
| update | redis_py | 4 | 0.426 | 0.420 | 1.01× | 1.00× |
| update | unmanaged | 1 | 4.466 | 4.051 | 1.10× | 16.44× |
| update | unmanaged | 4 | 9.201 | 8.227 | 1.12× | 19.61× |

[JSON](comparison.json) and [CSV](comparison.csv) retain all 42 exact combinations, p50/p95, finite throughput, sample counts and both builds' redis-py multiples. [Baseline](baseline.json) and [candidate](candidate.json) retain every warmup/measured sample and validation result.

## Sources, environment and scope

Baseline source `0b21647831d13b0c10e652b7095515b0f5a74069`; frozen candidate `c63477a9cb2509b3b3c484f0c6401e31b692ded9`. Both are clean, versioned native driver/CLI builds. Build and harness identities plus post-run binary hashes are in [provenance](provenance.json). Both builds completed before timing; matrices ran baseline then candidate consecutively, with no Go tests/builds or other heavy tasks between or during them. This remains sequential, not randomized interleaved A/B evidence. Production code was not changed after measurement.

Actual Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808, active/effective 4× QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard, container four CPUs/eight GiB. Certificate/hostname-verified data TLS and RESP3. Local management uses self-signed bootstrap trust mode. Host Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Native client CPU/RAM uncapped and GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset. [Environment](environment.json) records actual versions/resources.

Synthetic int64/string data has no NULL/floating-point edges; separate correctness tests cover boundaries. No production SLA, capacity claim, new ingestion/transfer measurement, multi-statement transaction, native Cluster API topology, ClickBench, Flex or Flight SQL measurement. Historical artifacts/deck results are unchanged.

The separate [managed diagnostic](../../managed-point-images/2026-11-14/report.md) retains 192 observations and its own earlier sources, including slower INSERT timings. Earlier unmanaged retrieval-only/Go SQL and instrumented point-read timings in the SDLC records do not become native results for this final guarded source.

## Verification, failures and cleanup

[Checks](checks.json) retains commands, actual checkpoint commits and all skip names: ordinary Software **1,164 PASS / 374 SKIP / 0 FAIL** (snapshot URI intentionally unset); full snapshot **306 / 62 / 0**; final focused race **91 / 2 / 0**, including acquisition/descriptor proofs and native projected-value boundary. The full snapshot source predates only the unmanaged refusal-token recognition correction; the final race checkpoint tests that correction against native Software. Two final race skips require separate owned EVAL-only/reader-lease operator fixtures. Optional topology, backups, profiling and capability skips do not prove compatibility. Eleven offline comparison/audit tests and the independent managed-diagnostic audit pass.

The pre-final live boundary run **failed**: 88 PASS / 2 SKIP / 1 FAIL because Redis returned `ERR ADBC_UNMANAGED_PROJECTION_TOO_LARGE` while the helper accepted only the bare token. [The retained failing checkpoint](pre-fix-regression.json) gives its actual source and outcome. The correction accepts only four exact native tokens, bare or with one `ERR ` prefix; altered/wrapped/joined failures remain terminal. The final 91-pass race run verifies the corrected native fallback. An earlier cross-review also reproduced and fixed composed `Nil`/transport errors being treated as absence; every pipeline outcome is now inspected before any repair/fallback. Agent review found no remaining actionable findings; human maintainer approval remains separate.

All Go/native processes and clients exited before owner-checked data/index clearing. A final empty-data/index and QPF/TLS/RESP3 gate passed. [Cleanup](cleanup.json) confirms owned container/network and generated environment/credentials removed while preexisting resources and TLS files were preserved. Operational identifiers, credentials/licenses and raw logs remain private.

Recalculate offline:

```sh
python3 benchmarks/oltp-query-efficiency/audit_pair.py \
  --require-check ordinary-software --require-check snapshot-software \
  --require-check final-race-software
```

Frozen harness copies are included in [harness](harness/). Future live runs require a new owned empty Redis Software fixture created through `make -C go redis-up`, verified 4× QPF and the documented resources. Do not infer current measurements from this archive.
