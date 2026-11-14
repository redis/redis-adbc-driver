# Selected native performance changes — November 14, 2026

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Both complete native matrices validated **168 records** each: 42 warmups and
126 measured trials, with zero failed results, serialization conflicts, retries
or discarded native records. The pair retains **5,472 measured operations** and
all **42 comparisons** against each build's redis-py controls.

This compares the first PR candidate plus its CI-test correction with the final
selected follow-up. The [first comparison](../2026-11-14/report.md) retains the
main→first-candidate results. The [experimental full matrix](../2026-11-14-followup/report.md)
retains the intermediate root-prefetch candidate. Those sources and raw observations
are unchanged; do not compose ratios across separate runs.

## Selected implementation

Filtered unmanaged reads use native HMGET. A non-NULL value, including an empty
string, proves the HASH existed for that atomic image. An all-NULL vector uses a
later complete HGETALL image; images are never merged, and existing residuals run
against the emitted row. Native reads retain NoRetry, source-key ownership checks,
exact values, missing/wrong-type policy and whole-pipeline error inspection before
fallback. Exact key-level permission refusals affect only that row; a command-wide
refusal can select scan-local compatibility. No per-row Lua/TYPE/HSTRLEN work or
Lua value copying remains on this path. Fanout is unchanged.

Managed publication safety now requires direct native error proofs. A joined or
wrapped permission error carrying reply loss cannot rebuild an index or select
readiness polling. The CI cleanup test schedules the actual reopened worker while
retaining its original deadline and all data/ownership assertions.

**Neither root-map prefetch nor publication batching is selected.** Both complete
experiments remain archived, including gains and regressions. Root-prefetch pilot
p50 gains did not establish reliable broader/tail improvement, and the full matrix
also had drifting controls. Publication batching's native results were mixed.
This is conservative selection, not proof either caused every observed regression.
The final metadata proof/read files match 31426 exactly; original standalone
publication directory/FT.INFO reads remain. No new options, cache or storage format.

## Observed changes

- Unmanaged `filtered_projection`, 1 client(s): **4.655→4.291 ms**, 1.08× baseline/candidate; candidate p95 10.913 ms.
- Unmanaged `filtered_projection`, 4 client(s): **11.602→10.800 ms**, 1.07× baseline/candidate; candidate p95 16.542 ms.
- Unmanaged `filtered_count_sum`, 1 client(s): **106.645→86.399 ms**, 1.23× baseline/candidate; candidate p95 108.898 ms.
- Unmanaged `filtered_count_sum`, 4 client(s): **184.006→99.741 ms**, 1.84× baseline/candidate; candidate p95 123.726 ms.

All observed slower driver p50 cases are retained, including small differences:

- managed `filtered_count_sum`, 4 client(s): 10.531→15.537 ms (+47.5%).
- unmanaged `grouped_count_sum`, 4 client(s): 435.425→450.262 ms (+3.4%).
- managed `delete`, 4 client(s): 56.899→61.188 ms (+7.5%).
- unmanaged `delete`, 4 client(s): 7.843→7.990 ms (+1.9%).
- unmanaged `insert`, 1 client(s): 3.963→4.013 ms (+1.3%).
- unmanaged `insert`, 4 client(s): 7.825→8.027 ms (+2.6%).
- managed `point_read`, 1 client(s): 2.769→2.882 ms (+4.1%).
- unmanaged `point_read`, 1 client(s): 2.766→2.851 ms (+3.1%).
- unmanaged `point_read`, 4 client(s): 5.588→5.829 ms (+4.3%).
- unmanaged `update`, 1 client(s): 4.002→4.320 ms (+8.0%).

The JSON/CSV exposes p95 and throughput as well as medians. Three finite trials
are not confidence intervals or causal attribution. Managed hot-path performance
was not optimized by these selected follow-ups; case/control drift must not be
presented as a managed speedup. The driver still has substantial OLTP overhead
versus direct client commands, which provide different guarantees.

P95 increases above 10% in this finite pair (all exact values remain in JSON/CSV):

- managed `filtered_count_sum`, 4 client(s): 15.627→29.818 ms (+90.8%).
- managed `grouped_count_sum`, 4 client(s): 25.036→32.203 ms (+28.6%).
- managed `delete`, 4 client(s): 663.008→866.439 ms (+30.7%).
- managed `insert`, 1 client(s): 140.395→154.822 ms (+10.3%).
- managed `insert`, 4 client(s): 696.024→925.066 ms (+32.9%).
- managed `point_read`, 1 client(s): 3.151→4.043 ms (+28.3%).
- unmanaged `update`, 1 client(s): 5.429→8.554 ms (+57.5%).
- unmanaged `update`, 4 client(s): 17.419→19.343 ms (+11.0%).

## Complete comparison

Point fixture: 1,000 rows. Query fixture: 100,000 rows. Four public int64/string
columns plus ordinal control field. Projection returns 100 rows, filtered COUNT/SUM
matches 10,000, grouped COUNT/SUM returns 16 groups; aggregate outputs cast to BIGINT.
Managed uses known internal row IDs; unmanaged/raw use known application keys.
Address mapping is outside timing. Execute plus complete typed Arrow fetch or
acknowledged mutation is timed; setup/teardown and independent validation are not.
Application clients persist; internal driver setup remains included. Closed-loop
operations/s are finite workload throughput, not saturated capacity.

Pooled p50 milliseconds below: baseline/candidate below 1× means slower, candidate/
redis-py below 1× means faster. Index/layout and consistency guarantees differ.

| Operation | Mode | Clients | Baseline ms | Candidate ms | Baseline/candidate | Candidate/redis-py |
|---|---|---:|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 4.130 | 3.979 | 1.04× | 1.41× |
| filtered_count_sum | managed | 4 | 10.531 | 15.537 | 0.68× | 1.56× |
| filtered_count_sum | redis_py | 1 | 2.862 | 2.828 | 1.01× | 1.00× |
| filtered_count_sum | redis_py | 4 | 9.731 | 9.951 | 0.98× | 1.00× |
| filtered_count_sum | unmanaged | 1 | 106.645 | 86.399 | 1.23× | 30.55× |
| filtered_count_sum | unmanaged | 4 | 184.006 | 99.741 | 1.84× | 10.02× |
| filtered_projection | managed | 1 | 17.051 | 16.316 | 1.05× | 14.96× |
| filtered_projection | managed | 4 | 20.486 | 17.800 | 1.15× | 3.74× |
| filtered_projection | redis_py | 1 | 1.117 | 1.091 | 1.02× | 1.00× |
| filtered_projection | redis_py | 4 | 5.126 | 4.762 | 1.08× | 1.00× |
| filtered_projection | unmanaged | 1 | 4.655 | 4.291 | 1.08× | 3.93× |
| filtered_projection | unmanaged | 4 | 11.602 | 10.800 | 1.07× | 2.27× |
| grouped_count_sum | managed | 1 | 11.963 | 11.924 | 1.00× | 0.35× |
| grouped_count_sum | managed | 4 | 19.280 | 18.514 | 1.04× | 0.17× |
| grouped_count_sum | redis_py | 1 | 34.272 | 34.349 | 1.00× | 1.00× |
| grouped_count_sum | redis_py | 4 | 105.064 | 108.003 | 0.97× | 1.00× |
| grouped_count_sum | unmanaged | 1 | 392.878 | 387.496 | 1.01× | 11.28× |
| grouped_count_sum | unmanaged | 4 | 435.425 | 450.262 | 0.97× | 4.17× |
| delete | managed | 1 | 42.741 | 41.043 | 1.04× | 188.09× |
| delete | managed | 4 | 56.899 | 61.188 | 0.93× | 153.43× |
| delete | redis_py | 1 | 0.231 | 0.218 | 1.06× | 1.00× |
| delete | redis_py | 4 | 0.372 | 0.399 | 0.93× | 1.00× |
| delete | unmanaged | 1 | 3.990 | 3.913 | 1.02× | 17.93× |
| delete | unmanaged | 4 | 7.843 | 7.990 | 0.98× | 20.04× |
| insert | managed | 1 | 43.503 | 41.919 | 1.04× | 174.63× |
| insert | managed | 4 | 52.018 | 51.110 | 1.02× | 129.55× |
| insert | redis_py | 1 | 0.250 | 0.240 | 1.04× | 1.00× |
| insert | redis_py | 4 | 0.428 | 0.395 | 1.08× | 1.00× |
| insert | unmanaged | 1 | 3.963 | 4.013 | 0.99× | 16.72× |
| insert | unmanaged | 4 | 7.825 | 8.027 | 0.97× | 20.35× |
| point_read | managed | 1 | 2.769 | 2.882 | 0.96× | 12.21× |
| point_read | managed | 4 | 5.532 | 5.487 | 1.01× | 12.34× |
| point_read | redis_py | 1 | 0.240 | 0.236 | 1.02× | 1.00× |
| point_read | redis_py | 4 | 0.437 | 0.445 | 0.98× | 1.00× |
| point_read | unmanaged | 1 | 2.766 | 2.851 | 0.97× | 12.08× |
| point_read | unmanaged | 4 | 5.588 | 5.829 | 0.96× | 13.11× |
| update | managed | 1 | 45.925 | 44.770 | 1.03× | 182.07× |
| update | managed | 4 | 69.953 | 61.984 | 1.13× | 150.50× |
| update | redis_py | 1 | 0.232 | 0.246 | 0.94× | 1.00× |
| update | redis_py | 4 | 0.406 | 0.412 | 0.99× | 1.00× |
| update | unmanaged | 1 | 4.002 | 4.320 | 0.93× | 17.57× |
| update | unmanaged | 4 | 7.762 | 7.734 | 1.00× | 18.78× |

[JSON](comparison.json) and [CSV](comparison.csv) retain all cases, exact pooled
p50/p95, finite throughput, samples and both builds' redis-py multiples.
[Baseline](baseline.json) and [candidate](candidate.json) retain all raw records.

## Scope, environment and checks

Frozen baseline `31426a96866c4bc12f7f691b3b1de23a6af9dbef`; selected candidate `c0d9f1e76716ffc87f3f1176a98527740571d508`.

Clean versioned native driver/CLI builds finished before timing. Matrices ran
baseline then candidate consecutively with no competing task-owned tests, builds
or heavy work. This is not randomized/interleaved evidence. Post-run binaries and
identical frozen harness match [provenance](provenance.json). Telemetry explicitly
disabled for both builds; native GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset and client
CPU/RAM uncapped. No selected production changes followed measurement.

Actual Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808; active/effective 4× QPF,
six workers, mnp/32, one 4-GiB noeviction RAM shard; container 4 CPUs / 8 GiB.
Certificate/hostname-verified data TLS and RESP3. Local management self-signed
bootstrap trust mode. Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Selected
actual versions/resources are in [environment](environment.json).

[Checks](checks.json) records actual source, commands, every skipped test and
counts: selected-source verified Software race 99 PASS / 2 SKIP / 0 FAIL; unit-only all
Go packages 660 / 895 / 0 (URIs unset, not compatibility proof). The earlier experimental
source 3f9 passed 319 snapshot tests / 62 skips; that result is not relabeled as this
selected source. Metadata files restore the fully CI-verified baseline exactly;
final selected-head driver/snapshot/integration CI is tracked in PR #439. Optional
operator/topology/backup/profile/capability skips do not establish compatibility.

Independent review covers exact int64/NULL/empty/NUL values, complete later row
images on live changes, key-specific ACL model outcomes, strict direct native
refusals, no uncertain replay, cancellation, source ownership and original managed
publication proofs. Selected guards passed combined checks. Scoped pilots keep
their own sources; all arithmetic/source/build/input/manifest audits pass. Agent
review is not maintainer approval. Full remote CI for the earlier cleanup-test
fix passed in [run 43645143585](https://github.com/redis/redis-adbc-driver/actions/runs/43645143585);
final evidence-head verification is reported in the PR body.

Dense synthetic data has no NULL/floating-point edges. All-selected-NULL/sparse
rows require an extra read and may fetch large unselected fields through HGETALL;
these gains do not establish sparse/wide performance. Server CPU attribution was
unavailable; Go diagnostic elapsed/allocations are not Redis CPU measurements.
No new ingestion/transfer, ClickBench, Flex, Flight SQL, multi-statement transaction
or native Cluster API measurement. Historical results/deck numbers are unchanged.

Every Go/native process and client exited before owner-checked clearing and a final
empty/QPF/TLS/RESP3 gate. [Cleanup](cleanup.json) confirms only owned container/
network and generated environment/credentials removed; preexisting resources/TLS
preserved. Operational IDs, credentials/licenses and raw private logs are excluded.

```sh
python3 benchmarks/oltp-query-efficiency/audit_pair.py \
 --bundle benchmarks/oltp-query-efficiency/2026-11-14-final \
 --require-check integrated-race-software --require-check unit-only-all-packages
```
