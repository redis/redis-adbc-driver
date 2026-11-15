# Native Linux confirmation of optional writer scheduling

Date: 2026-11-14 (coordinator local date). This is the normal Python/ADBC shared-library path on a Linux/arm64 client near Redis Software. Baseline source **`d7c0978f29ee57ee7d18fc4f67e2dba2b992d0ab`** uses legacy/default admission. Candidate source **`0884e3c893e9c7517a9f686492204b55d1fb6eec`** explicitly enables and verifies `adbc.redis.writer_admission_queue=true` on every driver connection. The option remains **default false**. These are not timings of agent commits `03e04002` or `c019a393`.

**The result is a fairness/throughput choice, not a general latency win.** At four contending writers, candidate whole-trial completion throughput increases and p95 decreases, while pooled medians increase roughly six to seven times and operation means increase about one-third. One-writer results are close and mixed. All raw redis-py controls and forward/reverse pairs remain available; no control-normalized or saturated-capacity claim is made.

[Raw A1](A1.json), [B1](B1.json), [B2](B2.json), [A2](A2.json), [complete comparison JSON](comparison.json), [CSV](comparison.csv), [frozen builds/harnesses](frozen-builds.json), [window validation/environment/cleanup](windows.json), [retry-policy scope](retry-policy-audit.json), [hash manifest](sha256.json).

## Measurement and complete accounting

Both native libraries and CLIs were built before any native timing. The unchanged public wrapper and existing scenario helpers were frozen with SHA-256 hashes. Four windows ran consecutively in A1/B1/B2/A2 order without source, library or harness changes; coordinator checks verified hashes before/after. Each window has 48 records: INSERT/UPDATE/DELETE × managed/redis-py × 1/4 clients × one warmup plus three measured repetitions. Every trial performs 32 operations over a fresh 1,000-row fixture.

**All 192 records and 6,144 acknowledged operations validated**, including 48 warmup records/1,536 operations. The measured subset is 144 records/4,608 operations; each pooled operation/mode/client cell has 192 measured latency observations. The frozen harness executed its complete typed SQL/final-state oracles before recording success. The independent offline audit verifies the exact trial grid, duplicates/omissions, success markers, latency counts, reported per-trial statistics, QPF gates and source/library/wrapper identities; it does not re-query deleted fixtures after cleanup.

Persistent per-thread client/cursor objects are created before the timing barriers, then normal SQL execute/acknowledgement or Redis commands are timed. Independent validation and object teardown are outside timing. **Lazy first-operation transport work remains included**: Redis-py construction/context entry does not pre-connect its socket, and the driver can also lazily create/dial an execution pool. Fresh client objects are used for each trial, so a warmup repetition does not prewarm the later repetitions’ client pools. The original raw labels are preserved; no fully prewarmed-connection timing claim follows.

## Pooled results

| Operation | Mode | Clients | Throughput B→C ops/s | p50 B→C ms | p95 B→C ms | Mean B→C ms |
|---|---|---:|---:|---:|---:|---:|
| INSERT | managed | 1 | 81.15→80.52 | 5.746→5.719 | 33.566→31.820 | 12.322→12.418 |
| INSERT | managed | 4 | 73.12→80.07 | 6.677→47.639 | 199.404→75.660 | 35.512→48.092 |
| INSERT | redis_py | 1 | 8387.10→8969.27 | 0.048→0.046 | 0.130→0.113 | 0.118→0.111 |
| INSERT | redis_py | 4 | 5460.42→5921.89 | 0.193→0.149 | 4.295→4.215 | 0.677→0.621 |
| UPDATE | managed | 1 | 80.48→81.64 | 6.226→6.249 | 32.831→32.612 | 12.423→12.247 |
| UPDATE | managed | 4 | 69.73→83.09 | 7.572→46.340 | 283.513→69.951 | 35.517→46.310 |
| UPDATE | redis_py | 1 | 8977.21→8896.41 | 0.044→0.045 | 0.107→0.105 | 0.111→0.112 |
| UPDATE | redis_py | 4 | 6063.41→5543.14 | 0.142→0.169 | 4.074→4.239 | 0.607→0.663 |
| DELETE | managed | 1 | 81.05→82.22 | 5.799→5.768 | 33.951→33.215 | 12.337→12.161 |
| DELETE | managed | 4 | 69.03→81.70 | 7.123→46.686 | 194.559→74.477 | 34.327→46.978 |
| DELETE | redis_py | 1 | 9343.33→9305.63 | 0.042→0.042 | 0.096→0.119 | 0.106→0.107 |
| DELETE | redis_py | 4 | 6372.02→6040.74 | 0.111→0.148 | 3.842→3.941 | 0.574→0.611 |

Throughput is total completed operations divided by summed trial elapsed time, not the inverse of pooled median latency or whole-process runtime. Means weight every timed operation equally. Percentiles use nearest rank except p50, which uses the usual even-sample median. p99, maxima, every trial duration and all raw latency arrays remain in JSON/CSV. Managed SQL provides snapshot/publication checks that single-HASH Redis commands do not; their guarantees differ.

## Forward and reverse pairs, including controls

| Pair | Operation | Mode | Clients | Throughput B→C ops/s | p50 B→C ms | p95 B→C ms |
|---|---|---|---:|---:|---:|---:|
| A1→B1 | INSERT | managed | 1 | 81.75→79.21 | 5.776→5.747 | 40.953→31.613 |
| A1→B1 | INSERT | managed | 4 | 76.77→80.24 | 6.668→47.109 | 196.867→76.675 |
| A1→B1 | INSERT | redis_py | 1 | 8628.31→8885.80 | 0.048→0.046 | 0.121→0.143 |
| A1→B1 | INSERT | redis_py | 4 | 5416.56→6035.16 | 0.176→0.130 | 4.331→4.215 |
| A1→B1 | UPDATE | managed | 1 | 79.77→81.55 | 6.284→6.162 | 31.782→40.253 |
| A1→B1 | UPDATE | managed | 4 | 66.75→83.88 | 7.622→45.806 | 283.513→69.951 |
| A1→B1 | UPDATE | redis_py | 1 | 9207.97→8753.83 | 0.044→0.045 | 0.087→0.102 |
| A1→B1 | UPDATE | redis_py | 4 | 6147.10→5404.53 | 0.150→0.164 | 4.074→4.519 |
| A1→B1 | DELETE | managed | 1 | 81.20→82.33 | 5.996→5.768 | 40.263→33.407 |
| A1→B1 | DELETE | managed | 4 | 68.46→82.85 | 7.343→45.967 | 180.754→71.684 |
| A1→B1 | DELETE | redis_py | 1 | 9277.23→9248.89 | 0.042→0.042 | 0.096→0.144 |
| A1→B1 | DELETE | redis_py | 4 | 6191.35→6095.25 | 0.140→0.153 | 3.970→3.941 |
| B2→A2 (C first) | INSERT | managed | 1 | 80.55→81.87 | 5.672→5.716 | 33.306→32.306 |
| B2→A2 (C first) | INSERT | managed | 4 | 69.81→79.91 | 6.684→49.443 | 280.873→75.549 |
| B2→A2 (C first) | INSERT | redis_py | 1 | 8159.02→9054.33 | 0.048→0.046 | 0.167→0.091 |
| B2→A2 (C first) | INSERT | redis_py | 4 | 5504.99→5812.79 | 0.200→0.162 | 4.273→4.229 |
| B2→A2 (C first) | UPDATE | managed | 1 | 81.21→81.73 | 6.122→6.276 | 41.355→31.509 |
| B2→A2 (C first) | UPDATE | managed | 4 | 72.99→82.32 | 7.374→47.253 | 286.145→69.983 |
| B2→A2 (C first) | UPDATE | redis_py | 1 | 8757.73→9043.70 | 0.045→0.045 | 0.124→0.169 |
| B2→A2 (C first) | UPDATE | redis_py | 4 | 5981.98→5689.04 | 0.133→0.176 | 4.084→4.239 |
| B2→A2 (C first) | DELETE | managed | 1 | 80.90→82.11 | 5.614→5.796 | 32.330→33.215 |
| B2→A2 (C first) | DELETE | managed | 4 | 69.61→80.59 | 6.616→46.751 | 196.582→74.614 |
| B2→A2 (C first) | DELETE | redis_py | 1 | 9410.38→9363.07 | 0.042→0.042 | 0.100→0.099 |
| B2→A2 (C first) | DELETE | redis_py | 4 | 6563.54→5987.18 | 0.086→0.141 | 3.820→4.050 |

The arrows in numeric cells always mean baseline→candidate even when execution order is reversed. Redis-py controls drift in both directions across operations/pairs. The complete comparison retains all 36 pooled/per-pair cases, including slower results; finite repetitions are not confidence intervals or causal server-CPU attribution.

## Environment and topology

Client: Linux/arm64, Python 3.14.10, Go-built native library 1.27.3, four CPU/4 GiB container allocation. Pinned packages recorded in every raw file: pyarrow 26.0.1, adbc-driver-manager 1.13.0, redis-py 8.1.0, hiredis 3.5.0. The client ran near the same Redis Software TLS endpoint, avoiding the macOS host-published-port path. Server: Redis Software with independently verified active/effective 4× QPF, six Search workers, mnp/32, one 4 GiB RAM shard, noeviction, strict certificate/hostname TLS and RESP3; server container four CPU/8 GiB. The coordinator’s [same-fixture final environment observation](../../performance-round3/2026-11-14/environment.json) supplements the original gates.

The earlier macOS Go prototype data remains historical and explicitly scoped to that host/published-port route. A separate Linux control did not reproduce its ~13 ms post-proof response delay. **Do not transfer absolute macOS latencies, the claimed cause of that delay, or ratios across topologies to these Linux measurements.** This native comparison has its own exact sources, binaries, clients, controls and oracles.

The original `automatic_retries=0` field is not a measured wire retry/reconnect counter. The harness has no caller SQL/command retry loop; redis-py’s exact from_url construction has a zero command retry budget in the pinned 8.1.0 implementation, confirmed by separate [post-run Linux no-network policy introspection](post-run-retry-policy.json), but connection management can reconnect. No command-failure/retry telemetry was instrumented. See the separate post-run audit; original values and file hashes are retained.

Every window completed with no recorded failed trial and successful owned cleanup. All native clients/processes ended; each transient client container and generated environment was removed. Whole-process durations in windows.json include setup/validation/cleanup and must not be reported as query throughput. Root owns final server-fixture cleanup. No default behavior was changed to queue mode, no cross-process/global FIFO is promised, and human approval is not inferred.

Reproduce the offline audit/statistics with `python3 benchmarks/managed-writer-handoff/native-linux/compare.py`. Runtime reruns require an exclusively owned, verified Software 4× QPF fixture and newly frozen source/binary labels; do not silently reuse this dataset’s labels.
