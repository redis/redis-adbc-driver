# Native performance follow-up — November 14, 2026

**NOT SELECTED for root-map prefetch.** These are frozen experimental-source measurements. The broader matrix shows material tail/mixed-workload regressions and drifting controls, so the final selected tree omits prefetch. This does not prove prefetch caused every regression. All measurements remain unchanged; a fresh final comparison is recorded separately.

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Both complete matrices validated **168 records**: 42 warmups and 126 measured
trials per build, zero failed results, conflicts, retries or discarded native
records. The pair retains **5,472 measured operations**. Point cases contain 96
measured samples/build; query cases 24. All 42 combinations retain both builds'
redis-py controls, exact p50/p95 and finite closed-loop throughput.

This follow-up compares the first PR #439 candidate (plus its test-only CI fix)
with the final selected changes. The [earlier comparison](../2026-11-14/report.md)
retains the original main→first-candidate evidence. Do not multiply ratios from
these separate experiments or relabel their sources.

## Selected changes

- Native projected unmanaged reads replace per-row Lua. A non-NULL HMGET value,
  including an empty string, proves HASH existence for that atomic row image.
  All-NULL vectors use a later complete HGETALL image, never a merge of images;
  the existing residual predicate is applied afterward. Native reads retain
  NoRetry. Inspect every command and outer pipeline before any fallback. Exact
  per-key permission refusals affect only that row; command-wide refusals may
  select scan-local compatibility. Missing/wrong-type and external ownership
  policy remain. No Lua value copies or per-field HSTRLEN checks on this path.
- Bounded root-map retrieval shares the acquisition-proof pipeline and avoids
  one separate exchange. HLEN precedes full MEMORY USAGE SAMPLES 0 before Lua
  enumeration. Native ACL checks, all original proofs/error precedence and
  unknown-outcome rules remain; installation is per-pin only. Known restricted
  and shared readers retain their original path. No metadata/lease/permission
  cache across statements and no protocol/storage-format change.
- Managed publication fallbacks require direct native permission refusals.
  Joined/wrapped permission errors carrying a lost reply cannot rebuild indexes
  or select readiness polling. Real-validator regressions retain the diagnostics.

## Observed p50 changes

- managed `point_read`: one client 2.879→2.627 ms (1.10× baseline/candidate); four clients 5.492→5.112 ms (1.07× baseline/candidate).
- unmanaged `point_read`: one client 2.901→2.649 ms (1.09× baseline/candidate); four clients 5.460→5.200 ms (1.05× baseline/candidate).
- unmanaged `filtered_projection`: one client 5.317→4.764 ms (1.12× baseline/candidate); four clients 12.313→10.280 ms (1.20× baseline/candidate).
- unmanaged `filtered_count_sum`: one client 107.430→82.651 ms (1.30× baseline/candidate); four clients 192.297→81.154 ms (2.37× baseline/candidate).

Every slower case is retained below. Driver cases with any higher observed p50:

- managed `filtered_count_sum`, 1 client(s): 3.955→4.331 ms (+9.5%).
- managed `filtered_projection`, 1 client(s): 16.672→16.835 ms (+1.0%).
- managed `filtered_projection`, 4 client(s): 16.489→21.422 ms (+29.9%).
- unmanaged `grouped_count_sum`, 1 client(s): 395.073→397.889 ms (+0.7%).
- managed `update`, 1 client(s): 45.377→47.878 ms (+5.5%).

Three measured trials provide neither a confidence interval nor causal attribution.
Driver/raw-client operations provide different guarantees and use different
index/layout strategies; these are workload observations, not a product ranking
or saturated capacity. Native client commands do not supply managed statement
publication guarantees. The full table exposes remaining OLTP overhead.

## Complete comparison

Point fixture: 1,000 rows; query fixture: 100,000 rows. Four public int64/string columns
plus ordinal control field. Filtered projection returns 100 rows, filtered COUNT/SUM
matches 10,000, grouped COUNT/SUM returns 16 groups. Aggregate outputs cast to BIGINT.
Managed addressing uses known internal row IDs; unmanaged/raw use known application
keys. Address lookup is outside timing.

Timings are pooled p50 milliseconds through execute plus complete typed Arrow fetch
or acknowledged mutation. Application clients persist within trials; independent
validation and setup/teardown are excluded. Internal driver setup remains included.
Below 1× baseline/candidate means slower, below 1× candidate/redis-py means faster.

| Operation | Mode | Clients | Baseline ms | Candidate ms | Baseline/candidate | Candidate/redis-py |
|---|---|---:|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 3.955 | 4.331 | 0.91× | 1.42× |
| filtered_count_sum | managed | 4 | 11.178 | 10.581 | 1.06× | 1.10× |
| filtered_count_sum | redis_py | 1 | 2.910 | 3.046 | 0.96× | 1.00× |
| filtered_count_sum | redis_py | 4 | 10.430 | 9.581 | 1.09× | 1.00× |
| filtered_count_sum | unmanaged | 1 | 107.430 | 82.651 | 1.30× | 27.13× |
| filtered_count_sum | unmanaged | 4 | 192.297 | 81.154 | 2.37× | 8.47× |
| filtered_projection | managed | 1 | 16.672 | 16.835 | 0.99× | 14.28× |
| filtered_projection | managed | 4 | 16.489 | 21.422 | 0.77× | 4.53× |
| filtered_projection | redis_py | 1 | 1.097 | 1.179 | 0.93× | 1.00× |
| filtered_projection | redis_py | 4 | 5.015 | 4.729 | 1.06× | 1.00× |
| filtered_projection | unmanaged | 1 | 5.317 | 4.764 | 1.12× | 4.04× |
| filtered_projection | unmanaged | 4 | 12.313 | 10.280 | 1.20× | 2.17× |
| grouped_count_sum | managed | 1 | 12.017 | 11.978 | 1.00× | 0.34× |
| grouped_count_sum | managed | 4 | 22.584 | 19.200 | 1.18× | 0.18× |
| grouped_count_sum | redis_py | 1 | 34.206 | 35.344 | 0.97× | 1.00× |
| grouped_count_sum | redis_py | 4 | 105.610 | 107.296 | 0.98× | 1.00× |
| grouped_count_sum | unmanaged | 1 | 395.073 | 397.889 | 0.99× | 11.26× |
| grouped_count_sum | unmanaged | 4 | 476.216 | 451.626 | 1.05× | 4.21× |
| delete | managed | 1 | 44.768 | 42.316 | 1.06× | 178.91× |
| delete | managed | 4 | 59.848 | 49.922 | 1.20× | 137.74× |
| delete | redis_py | 1 | 0.229 | 0.237 | 0.97× | 1.00× |
| delete | redis_py | 4 | 0.383 | 0.362 | 1.06× | 1.00× |
| delete | unmanaged | 1 | 4.068 | 3.790 | 1.07× | 16.03× |
| delete | unmanaged | 4 | 8.148 | 7.452 | 1.09× | 20.56× |
| insert | managed | 1 | 44.783 | 43.375 | 1.03× | 170.49× |
| insert | managed | 4 | 56.370 | 43.792 | 1.29× | 108.47× |
| insert | redis_py | 1 | 0.249 | 0.254 | 0.98× | 1.00× |
| insert | redis_py | 4 | 0.445 | 0.404 | 1.10× | 1.00× |
| insert | unmanaged | 1 | 4.021 | 3.780 | 1.06× | 14.86× |
| insert | unmanaged | 4 | 7.816 | 7.046 | 1.11× | 17.45× |
| point_read | managed | 1 | 2.879 | 2.627 | 1.10× | 10.79× |
| point_read | managed | 4 | 5.492 | 5.112 | 1.07× | 11.05× |
| point_read | redis_py | 1 | 0.260 | 0.243 | 1.07× | 1.00× |
| point_read | redis_py | 4 | 0.501 | 0.463 | 1.08× | 1.00× |
| point_read | unmanaged | 1 | 2.901 | 2.649 | 1.09× | 10.88× |
| point_read | unmanaged | 4 | 5.460 | 5.200 | 1.05× | 11.24× |
| update | managed | 1 | 45.377 | 47.878 | 0.95× | 176.97× |
| update | managed | 4 | 61.824 | 53.344 | 1.16× | 120.13× |
| update | redis_py | 1 | 0.254 | 0.271 | 0.94× | 1.00× |
| update | redis_py | 4 | 0.411 | 0.444 | 0.92× | 1.00× |
| update | unmanaged | 1 | 4.030 | 3.954 | 1.02× | 14.61× |
| update | unmanaged | 4 | 7.663 | 7.495 | 1.02× | 16.88× |

[Comparison JSON](comparison.json) and [CSV](comparison.csv) retain all 42 cases,
p50/p95, throughput, sample counts and both builds' redis-py multiples.
[Baseline](baseline.json) and [candidate](candidate.json) retain every raw record.

## Experiments and limits

[Point pilot](../../point-root-prefetch/2026-11-14/report.md) retains 96 native ABBA
records and its actual earlier source; later restricted/queue-error guards are
covered by final integrated checks. [Unmanaged diagnostics](../../unmanaged-projection/2026-11-14/report.md)
retain all 32 SQL ABBA records / 1,024 queries and 16 retrieval observations. The Go SQL
profile replaces 320,000 source EVALSHA_RO commands/trial with 320,000 native HMGET;
**fanout is unchanged**. Exposed server CPU counters were unavailable/unusable;
Go elapsed/allocations do not establish Redis CPU attribution.

The [managed publication batching experiment](../../managed-publication-readiness/2026-11-14-native/report.md)
is **not selected** for production integration. It saves one exchange but mixed
native results include 30.3% higher four-client DELETE p50 despite 20.5% higher throughput
and lower p95. All 96 native records and 384 instrumented Go observations remain archived,
including improvements. Production retains the original separate directory and
FT.INFO reads; only direct-native error safety fixes remain.

The dense fixture has no NULL/floating-point edges. Correctness tests cover exact
int64, NULL, empty/NUL strings, missing/type changes, live row changes, ACL refusal
models, cancellation and lost replies. All-selected-NULL/sparse rows may need a
second read and full unselected fields through HGETALL; dense-fixture gains do not
establish sparse/wide-row performance. ACL-selector error handling is model-tested,
not a claim of a live Software operator ACL fixture. No new ingestion/transfer,
ClickBench, Flex, Flight SQL, multi-statement transaction or native Cluster API
measurement. Historical reports/deck numbers are unchanged.

## Environment, sources and verification

Baseline `31426a96866c4bc12f7f691b3b1de23a6af9dbef`; final frozen candidate `3f9cf6a85a2cf4b7744f785bca8517b3ff18859c`.

Both clean versioned driver/CLI builds finished before timing. Matrices ran
baseline then candidate consecutively, without competing task-owned Go tests,
builds or heavy work. This is sequential, not randomized/interleaved evidence.
Post-run binaries and frozen identical harness hashes match [provenance](provenance.json).
Production code did not change after measurement. Telemetry was explicitly disabled
for both final matrices.

Actual Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808; active/effective 4× QPF,
six Search workers, mnp/32, one 4-GiB noeviction RAM shard; container 4 CPUs / 8 GiB.
Certificate/hostname-verified data TLS and RESP3. Local management uses self-signed
bootstrap trust mode. Host Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64.
Native client CPU/RAM uncapped, GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset.
[Environment](environment.json) retains selected actual versions/resources.

[Checks](checks.json) retains actual source, command, counts and every skipped name:
combined verified Software race 108 PASS / 2 SKIP / 0 FAIL; unit-only all packages 668 / 896 / 0
(URIs intentionally unset, not compatibility proof); full separate Software
snapshot suite 319 / 62 / 0. Skips include optional operator/backup/topology/profile
fixtures and capabilities; no skip is compatibility evidence. All pilot/diagnostic
and final complete-pair audits pass, along with offline corruption tests,
formatting and diff checks. Independent final source review found no actionable
correctness/security issue. Agent review is not maintainer approval.

The [CI-test correction](https://github.com/redis/redis-adbc-driver/actions/runs/43645143585)
passed full remote CI at 31426a96 before these follow-ups. Remote CI for the final
evidence commit is tracked in PR #439; its status at publication does not relabel
that earlier run as validation of later production code.

All clients/processes exited before owner-checked data/index clearing and a final
empty/QPF/TLS/RESP3 gate. [Cleanup](cleanup.json) confirms owned container/network
and generated environment/credentials removed, with preexisting containers,
networks and TLS material preserved. Operational identifiers, credentials,
licenses and raw private logs are excluded.

Audit offline:

```sh
python3 benchmarks/oltp-query-efficiency/audit_pair.py \
 --bundle benchmarks/oltp-query-efficiency/2026-11-14-followup \
 --require-check integrated-race-software --require-check unit-only-all-packages \
 --require-check snapshot-software
```

## Independent tail/control review and selection

Managed four-client projection has p50 +29.9%, p95 +99.2%, throughput −48.0%.
Managed one-client point p95 rises 60.6%; unmanaged one-client/four-client point
p95 rises 5.1%/23.5%. Redis-py point p50 controls also improve 6–8%; normalized
driver/client point ratios improve only ~2.4% with one client and slightly worsen
with four. The earlier point-only ABBA pilot has nearly flat controls, but these
broader results do not establish a reliable general/tail win. Root prefetch is
therefore not selected, without attributing every regression to it.
The [fresh selected-source comparison](../2026-11-14-final/report.md) omits both
root prefetch and publication batching. Earlier raw data/source identities remain.
