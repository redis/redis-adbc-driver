# Full ClickBench run: Redis Software RAM / main 14b4a494

Status: complete with recorded limits; no full-suite Redis score. This run covers all 43 official queries, up to three trials
each, on one c8g.metal-48xl with all 99,997,497 rows and 105 typed columns.
It supplies the benchmark comparison and slide update tracked in
[issue #487](https://github.com/redis/redis-adbc-driver/issues/487).

The driver is built from merged commit
`14b4a4940e78bf5496a0532caf6b8e800d38dd2a`, including PR #497. Its reported version
is `v0.0.29+main.14b4a494`, library SHA-256
`1cc9a55002f2a1a77a40a7c6eaa4ad6b75d22f14028d0eadbf7b026f4e4426c4`.
This is a main build, not a published v0.0.29 release binary.

The run used the same loaded immutable unmanaged HASH dataset, Redis Software 8.2.6-58 /
Redis 8.8.0, three shards, verified effective 4× QPF (six Search workers,
mnp/32), noeviction, TLS/RESP3, compact HASHes and predicate indexes. Every trial
keeps the original 300-second wall deadline and 86,918-MiB client memory cap;
all 192 CPUs remain available. Private scratch directories are reclaimed after
each isolated trial. No other database benchmark runs on this host.

One-time ingestion was measured with released v0.0.29 (7,035.959 seconds),
not this main build. This run performs no ingestion and claims no main-build
load time. The earlier release and b06 diagnostic attempts remain separately
labeled; their timings are not merged into this result with 129 trial slots.

The full unmanaged RAM database backup is complete in private S3: three files,
68,144,059,084 bytes, exact versions and full SHA-256 readbacks verified.
S3 locations, manifests, credentials, infrastructure identifiers and license
files stay private. No restore-test claim is made here.

The pinned published comparison retains 129 exact-machine entries, with 118
complete finite/nonnegative hot series. Preserve reported zeros. Hot time is
min(trials 2, 3), requiring both independently valid outputs. Produce per-query
comparisons for every entry; withhold any full-suite ranking unless all 43
Redis hot results are valid. Matching machine types does not imply identical
software, tuning, durability, network route or run dates.

Run `python3 analyze.py` to audit the published source hashes and coverage.
The full query outputs and independent DuckDB validation are complete: 33 valid
trial outputs, 36 measured failures, and 60 explicitly skipped slots. There
were no correctness mismatches or unresolved reference checks.

Per the user’s updated run policy, a 300-second first-trial timeout is reported
in GitHub and trials 2 and 3 are skipped. Skipped slots are labeled explicitly
and retain null timings; they are not additional measured failures. Trials
already completed before this policy change remain unchanged. A full hot score
still requires independently valid trials 2 and 3 for every query.


## Final results

All 43 queries were covered. Eleven queries passed all three independent output
checks: Q1, Q2, Q8, Q20, and Q37–Q43. Thirty-one queries hit the 300-second
wall limit; Q33 hit the 86,918-MiB client memory limit in all three trials.
The driver’s earlier Q1 lease failure did not reproduce on this main build;
full-table COUNT remains approximately 89 seconds hot. This does not certify
that every historical lease failure is fixed.

A total of 69 trials executed: 33 valid outputs, 33 timeout trials, and three
memory-limit trials. Sixty repeat slots were skipped under the first-trial
300-second timeout policy. Q3’s additional repeats were already underway before
that policy change. All 32 failing queries have linked GitHub reports in
[query-issues.json](query-issues.json). Skips are policy decisions, not extra
measured failures. No timeout is substituted as a valid latency.

[run.json](run.json), [correctness.json](correctness.json), and
[run-raw/](run-raw/) retain timings, values, classifications and source hashes.
[query-summary.csv](query-summary.csv) covers all 129 trial slots.
[all-query-hot-seconds.csv](all-query-hot-seconds.csv) compares all 43 queries
with all 129 pinned exact-machine published entries; the pairwise form is
[per-query-comparisons.csv](per-query-comparisons.csv).
[comparison.json](comparison.json) records coverage and the withheld full score.

Hot time is min(trials 2, 3), only when both outputs are independently valid.
The following representative published baselines all use one c8g.metal-48xl;
their dates, settings and source hashes remain in [published/](published/).
A missing Redis result precludes a full-suite aggregate or ranking. Totals for
other products must not be compared against a partial Redis sum.

| Query | Redis hot seconds / limit | ClickHouse seconds | DuckDB seconds | PostgreSQL seconds | Issue |
|---|---:|---:|---:|---:|---|
| Q1 | 89.208258 | 0.001000 | 0.014000 | 1.695000 | — |
| Q2 | 3.879504 | 0.001000 | 0.031000 | 2.575000 | — |
| Q3 | 300-s timeout | 0.002000 | 0.025000 | 3.047000 | [#493](https://github.com/redis/redis-adbc-driver/issues/493) |
| Q4 | 300-s timeout | 0.001000 | 0.024000 | 2.049000 | [#494](https://github.com/redis/redis-adbc-driver/issues/494) |
| Q5 | 300-s timeout | 0.020000 | 0.077000 | 12.366000 | [#495](https://github.com/redis/redis-adbc-driver/issues/495) |
| Q6 | 300-s timeout | 0.039000 | 0.074000 | 23.610000 | [#496](https://github.com/redis/redis-adbc-driver/issues/496) |
| Q7 | 300-s timeout | 0.002000 | 0.024000 | 2.035000 | [#498](https://github.com/redis/redis-adbc-driver/issues/498) |
| Q8 | 4.027976 | 0.006000 | 0.024000 | 2.657000 | — |
| Q9 | 300-s timeout | 0.048000 | 0.095000 | 17.168000 | [#499](https://github.com/redis/redis-adbc-driver/issues/499) |
| Q10 | 300-s timeout | 0.050000 | 0.109000 | 19.653000 | [#500](https://github.com/redis/redis-adbc-driver/issues/500) |
| Q11 | 300-s timeout | 0.036000 | 0.051000 | 3.804000 | [#501](https://github.com/redis/redis-adbc-driver/issues/501) |
| Q12 | 300-s timeout | 0.034000 | 0.054000 | 4.095000 | [#502](https://github.com/redis/redis-adbc-driver/issues/502) |
| Q13 | 300-s timeout | 0.061000 | 0.080000 | 7.890000 | [#504](https://github.com/redis/redis-adbc-driver/issues/504) |
| Q14 | 300-s timeout | 0.065000 | 0.138000 | 9.103000 | [#505](https://github.com/redis/redis-adbc-driver/issues/505) |
| Q15 | 300-s timeout | 0.064000 | 0.090000 | 9.051000 | [#506](https://github.com/redis/redis-adbc-driver/issues/506) |
| Q16 | 300-s timeout | 0.030000 | 0.088000 | 12.388000 | [#507](https://github.com/redis/redis-adbc-driver/issues/507) |
| Q17 | 300-s timeout | 0.066000 | 0.132000 | 135.676000 | [#508](https://github.com/redis/redis-adbc-driver/issues/508) |
| Q18 | 300-s timeout | 0.008000 | 0.134000 | 4.464000 | [#509](https://github.com/redis/redis-adbc-driver/issues/509) |
| Q19 | 300-s timeout | 0.103000 | 0.222000 | 89.159000 | [#511](https://github.com/redis/redis-adbc-driver/issues/511) |
| Q20 | 0.010367 | 0.002000 | 0.021000 | 1.948000 | — |
| Q21 | 300-s timeout | 0.059000 | 0.146000 | 3.359000 | [#512](https://github.com/redis/redis-adbc-driver/issues/512) |
| Q22 | 300-s timeout | 0.022000 | 0.113000 | 4.150000 | [#513](https://github.com/redis/redis-adbc-driver/issues/513) |
| Q23 | 300-s timeout | 0.078000 | 0.713000 | 4.064000 | [#514](https://github.com/redis/redis-adbc-driver/issues/514) |
| Q24 | 300-s timeout | 0.030000 | 0.105000 | 3.587000 | [#515](https://github.com/redis/redis-adbc-driver/issues/515) |
| Q25 | 300-s timeout | 0.013000 | 0.034000 | 2.848000 | [#516](https://github.com/redis/redis-adbc-driver/issues/516) |
| Q26 | 300-s timeout | 0.006000 | 0.046000 | 2.783000 | [#517](https://github.com/redis/redis-adbc-driver/issues/517) |
| Q27 | 300-s timeout | 0.011000 | 0.036000 | 2.862000 | [#519](https://github.com/redis/redis-adbc-driver/issues/519) |
| Q28 | 300-s timeout | 0.031000 | 0.095000 | 5.084000 | [#520](https://github.com/redis/redis-adbc-driver/issues/520) |
| Q29 | 300-s timeout | 0.191000 | 0.539000 | 38.062000 | [#521](https://github.com/redis/redis-adbc-driver/issues/521) |
| Q30 | 300-s timeout | 0.011000 | 0.034000 | 10.539000 | [#523](https://github.com/redis/redis-adbc-driver/issues/523) |
| Q31 | 300-s timeout | 0.047000 | 0.093000 | 7.730000 | [#524](https://github.com/redis/redis-adbc-driver/issues/524) |
| Q32 | 300-s timeout | 0.074000 | 0.161000 | 9.012000 | [#525](https://github.com/redis/redis-adbc-driver/issues/525) |
| Q33 | Memory limit | 0.097000 | 0.376000 | 152.500000 | [#526](https://github.com/redis/redis-adbc-driver/issues/526) |
| Q34 | 300-s timeout | 0.168000 | 0.332000 | 35.330000 | [#527](https://github.com/redis/redis-adbc-driver/issues/527) |
| Q35 | 300-s timeout | 0.164000 | 0.348000 | 35.846000 | [#528](https://github.com/redis/redis-adbc-driver/issues/528) |
| Q36 | 300-s timeout | 0.022000 | 0.085000 | 9.276000 | [#529](https://github.com/redis/redis-adbc-driver/issues/529) |
| Q37 | 9.627286 | 0.047000 | 0.037000 | 3.481000 | — |
| Q38 | 8.786944 | 0.028000 | 0.023000 | 3.322000 | — |
| Q39 | 2.395194 | 0.041000 | 0.029000 | 2.889000 | — |
| Q40 | 13.635788 | 0.088000 | 0.061000 | 2.820000 | — |
| Q41 | 1.471935 | 0.015000 | 0.023000 | 4.623000 | — |
| Q42 | 1.588980 | 0.013000 | 0.026000 | 4.269000 | — |
| Q43 | 8.844688 | 0.011000 | 0.024000 | 2.963000 | — |

Slide 23 was updated in the existing native presentation with matched-instance
coverage and full-suite totals for the published baselines. Redis totals remain
N/A because only 11/43 hot queries are validated. The deck retains 28 slides
after the previously requested deletion; related counters and summary wording
were reconciled. All 28 rendered slides were inspected.

Cloud cleanup is verified in [cleanup.json](cleanup.json): zero remaining
task-owned compute instances or volumes; task access roles, security group and
expiry controller removed. The private unmanaged S3 backup and evidence remain.
