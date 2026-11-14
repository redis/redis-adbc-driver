# OLTP and selective-query efficiency — November 13, 2026

The complete baseline and candidate matrices each validated **168 records**: 42 warmups and 126 measured trials, with no failed results, serialization conflicts, retries or discarded native records. Together these contain 5,472 measured operation observations. Each point case has 96 observations per build (3×32); each query case has 24 (3×8).

## What changed

- An ADBC connection now reuses at most one fully drained internal execution pool. Active streams/executions remain exclusive; each gets a fresh cancellation/ownership lease. This removes repeated internal TLS/RESP3 setup on warm statements. Canceled/uncertain work, in-flight commands and failed transaction-state cleanup discard the pool. Every pipeline reply is checked; no uncertain SQL/input is replayed.
- Active production maintenance workers perform retired-object collection off the statement path. Workerless stores, explicit sweep and bounded capacity recovery retain their foreground protocols. Unmanaged index definitions are checked once under every fresh statement pin instead of twice.
- Identical SQL text reuses syntax only. Bindings clear and execution still resolves current catalog data/schema; prepared-read tests cover intervening UPDATE and ALTER. Existing known-key lookup was verified to be direct; no unsafe lease/metadata cache was introduced.
- Managed publication shares its authorized private descriptors across indexing/readiness, batches first directory discovery, avoids an empty terminal page, relies on a bounded stronger atomic clone proof, and skips abort only after confirmed publication. Index readiness, final cardinality, permissions and ownership proofs remain. One-row directory population uses three client exchanges instead of five.
- Immutable managed BIGINT reducers can use runtime exactness proofs. Rounded groups/extrema, unsafe count×magnitude sums, overflow and server group caps retain exact fallback. Live unmanaged data retains authoritative retrieval; direct accumulator/group inputs avoid extra decoded row maps.

## Complete observed latency comparison

Point fixture: 1,000 rows, four public columns plus ordinal control field. Query fixture: 100,000 rows with the same schema. Filter projection returns 100 rows; filtered COUNT/SUM matches 10,000; grouped COUNT/SUM returns 16 groups. SQL aggregate outputs are explicitly cast to BIGINT. Managed addressing uses known internal row IDs, unmanaged/raw addressing known application keys; address lookup is outside timing.

All timings below are pooled p50 milliseconds, execute through complete typed Arrow fetch or acknowledged mutation. Application clients persist within each trial; setup/teardown and independent validation are excluded. Internal driver connection work is included. Managed and application indexes differ by mode; this comparison does not isolate index layout from execution cost. Closed-loop operations/s are exported separately and are not saturated capacity.

| Operation | Mode | Clients | Baseline p50 ms | Candidate p50 ms | Baseline/candidate | Candidate/redis-py time |
|---|---|---:|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 52.498 | 4.510 | 11.64× | 1.59× |
| filtered_count_sum | managed | 4 | 97.796 | 11.835 | 8.26× | 1.23× |
| filtered_count_sum | redis_py | 1 | 2.969 | 2.838 | 1.05× | 1.00× |
| filtered_count_sum | redis_py | 4 | 11.194 | 9.594 | 1.17× | 1.00× |
| filtered_count_sum | unmanaged | 1 | 241.839 | 265.933 | 0.91× | 93.72× |
| filtered_count_sum | unmanaged | 4 | 143.786 | 115.884 | 1.24× | 12.08× |
| filtered_projection | managed | 1 | 23.417 | 18.999 | 1.23× | 16.74× |
| filtered_projection | managed | 4 | 26.733 | 28.805 | 0.93× | 5.85× |
| filtered_projection | redis_py | 1 | 1.172 | 1.135 | 1.03× | 1.00× |
| filtered_projection | redis_py | 4 | 4.965 | 4.923 | 1.01× | 1.00× |
| filtered_projection | unmanaged | 1 | 21.663 | 16.550 | 1.31× | 14.58× |
| filtered_projection | unmanaged | 4 | 23.275 | 16.856 | 1.38× | 3.42× |
| grouped_count_sum | managed | 1 | 558.377 | 12.503 | 44.66× | 0.37× |
| grouped_count_sum | managed | 4 | 754.062 | 20.954 | 35.99× | 0.20× |
| grouped_count_sum | redis_py | 1 | 36.290 | 33.857 | 1.07× | 1.00× |
| grouped_count_sum | redis_py | 4 | 108.331 | 105.519 | 1.03× | 1.00× |
| grouped_count_sum | unmanaged | 1 | 390.517 | 392.374 | 1.00× | 11.59× |
| grouped_count_sum | unmanaged | 4 | 503.163 | 450.397 | 1.12× | 4.27× |
| delete | managed | 1 | 69.742 | 52.918 | 1.32× | 222.11× |
| delete | managed | 4 | 181.613 | 66.264 | 2.74× | 173.61× |
| delete | redis_py | 1 | 0.235 | 0.238 | 0.99× | 1.00× |
| delete | redis_py | 4 | 0.423 | 0.382 | 1.11× | 1.00× |
| delete | unmanaged | 1 | 10.651 | 4.329 | 2.46× | 18.17× |
| delete | unmanaged | 4 | 16.568 | 8.994 | 1.84× | 23.56× |
| insert | managed | 1 | 65.385 | 40.578 | 1.61× | 163.51× |
| insert | managed | 4 | 134.049 | 53.440 | 2.51× | 129.97× |
| insert | redis_py | 1 | 0.241 | 0.248 | 0.97× | 1.00× |
| insert | redis_py | 4 | 0.488 | 0.411 | 1.19× | 1.00× |
| insert | unmanaged | 1 | 9.910 | 4.484 | 2.21× | 18.07× |
| insert | unmanaged | 4 | 17.628 | 8.762 | 2.01× | 21.31× |
| point_read | managed | 1 | 8.655 | 3.313 | 2.61× | 12.94× |
| point_read | managed | 4 | 14.178 | 6.263 | 2.26× | 11.97× |
| point_read | redis_py | 1 | 0.252 | 0.256 | 0.98× | 1.00× |
| point_read | redis_py | 4 | 0.493 | 0.523 | 0.94× | 1.00× |
| point_read | unmanaged | 1 | 8.937 | 3.354 | 2.66× | 13.10× |
| point_read | unmanaged | 4 | 14.093 | 6.271 | 2.25× | 11.98× |
| update | managed | 1 | 82.496 | 68.434 | 1.21× | 277.01× |
| update | managed | 4 | 198.190 | 93.076 | 2.13× | 210.57× |
| update | redis_py | 1 | 0.242 | 0.247 | 0.98× | 1.00× |
| update | redis_py | 4 | 0.487 | 0.442 | 1.10× | 1.00× |
| update | unmanaged | 1 | 9.712 | 4.400 | 2.21× | 17.81× |
| update | unmanaged | 4 | 17.082 | 9.125 | 1.87× | 20.64× |

[Comparison JSON](comparison.json) and [CSV](comparison.csv) retain all 42 combinations, exact p50/p95, sample counts, each build's redis-py multiple and finite-trial throughput. [Baseline](baseline.json) and [candidate](candidate.json) preserve every warmup/measured latency and validation result. Below 1× baseline/candidate means the candidate was slower; below 1× candidate/redis-py means less elapsed time than the client baseline.

## Interpretation and remaining work

Single-client known-row reads improved about 2.6×, and unmanaged mutations about 2.2–2.5×. Managed mutations improved 1.2–1.6× but still take approximately 41–68 ms and remain 164–277× the native command time in these cases; admission, staging and index publication remain important targets. Managed filtered COUNT/SUM improved 11.6× and grouped COUNT/SUM 44.7× versus the same driver baseline. The candidate managed grouped case takes 12.503 ms versus 33.857 ms for redis-py on this fixture; index/layout differences prohibit a universal product claim.

Unmanaged aggregate gains are mixed: four-client filtered/grouped cases improve 1.24×/1.12×, while one-client grouped is essentially flat. **One-client filtered COUNT/SUM is 10.0% slower (241.839→265.933 ms); managed four-client projection is 7.8% slower (26.733→28.805 ms).** These observations are retained, not excluded. No confidence interval or causal attribution follows from three finite trials; case order, server/client caches, scheduling, allocator state and temperature can matter.

The full-size unmanaged diagnostic confirms the actual `typed_aggregate_inputs` strategy, rather than inferring it from a microbenchmark. Its pinned/planned filtered phase still makes 10,000 HGETALL calls plus one Search aggregate/two cursor reads; about 0.235 of 0.252 seconds is client command waiting/decoding. Its grouped phase retrieves 100K rows through one aggregate/ten cursor reads, with about 0.351 of 0.397 seconds in command waiting/decoding. This is **client-side elapsed attribution, not Redis server CPU**. The vector microbenchmark improves decode/reduce work and allocation, but does not establish low query latency. Reducing live retrieval/fanout while preserving membership and exact values is the next unmanaged target. See the [sanitized unmanaged profile](unmanaged-aggregate-profile.json).

The [pool profile](statement-profile.json) independently observes no new dials on warm reused reads versus two dials per forced-cold read. It is an instrumented Go race-test diagnostic; its wall times are not native matrix results. The unmanaged profile also uses a workerless protocol fixture and GOMAXPROCS=4, unlike the production-style native matrix.

## Environment and provenance

Baseline driver source `ca6858065c4cf02418d55c3aa62eb583590fe252`; candidate production source `45e0ef69297ecc536b88f690ef5a781218e3cf80`. Candidate is an unreleased versioned development build. Frozen driver/CLI/harness hashes and post-run binary hashes are in [provenance](provenance.json). The later opt-in diagnostic test adds no production behavior and does not relabel these observations.

Actual Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808, active/effective 4× QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard; actual container four CPUs/eight GiB. Strict certificate/hostname-verified data TLS and RESP3. Local management bootstrap uses its self-signed trust mode. Same owned fixture for both matrices. Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Native client CPU/RAM uncapped, GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset. See [environment](environment.json).

This is synthetic int64/string data without NULLs or floating-point edges; separate correctness tests cover those boundaries. No production SLA/capacity, new bulk/transfer measurement, multi-statement transaction, native Cluster API run, full product ranking, 100M ClickBench, Flex or Flight SQL result is claimed. Historical deck/product results are unchanged. The full ordinary suite ran between initial baseline and candidate; the matrices are same-host observations, not interleaved randomized A/B runs.

## Verification and ownership cleanup

[Checks](checks.json) records actual sources, commands, outcomes and all skipped test names. Broad ordinary Software: 1,145 top-level passes, 367 skips, zero failures (snapshot URI deliberately unset). Final focused race Software: 33 passes, zero skips/failures. Separate full snapshot Software: 295 passes, 62 skips, zero failures. Skips include opt-in profiles, missing optional topology/backup environments and Software capability refusals; they are not compatibility proof. Later transaction-cleanup/value-capture guards were checked in the final focused selection and snapshot suite, rather than relabeling the earlier ordinary-suite source. Eight Python scenario/comparison tests pass. The 100K unmanaged attribution test passes with complete Arrow type/value checking.

Cross-review caught and fixed count-before-enumeration and stale/canceled/transaction-state pool hazards. WATCH/UNWATCH and MULTI/EXEC regressions fail on the preceding implementation and pass after correction. Checks cover original pipeline errors, lost replies/no replay, cancellation/Close, pin/schema freshness, source ownership, precision/NULL/overflow/group-cap fallback and complete mutation post-state. Human maintainer review remains pending; agent review is not approval.

All native/Go processes and clients ended before a final empty-data/index and QPF/TLS/RESP3 gate. [Cleanup](cleanup.json) confirms owned fixture/network and generated credentials/environment removed, with preexisting resources and TLS material preserved. Operational identifiers, credentials/licenses, raw logs and CPU profile remain private.

Regenerate charts offline:

```sh
python3 benchmarks/oltp-query-efficiency/compare.py \
  --baseline benchmarks/oltp-query-efficiency/2026-11-13/baseline.json \
  --candidate benchmarks/oltp-query-efficiency/2026-11-13/candidate.json \
  --out /private/new-comparison-directory
```

Run future native matrices with `benchmarks/table-modes/scenarios.py` on a new owned empty Software fixture, first configured by `make -C go redis-up` and independently verified for the stated settings. Supply versioned driver/CLI paths, version and a fresh private work directory; source/driver/harness identity and all raw trials are recorded. Do not infer current measurements from this archived bundle.
