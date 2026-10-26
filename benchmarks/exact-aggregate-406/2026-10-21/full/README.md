# Full-scale exact aggregate comparison

All **24 timed trials and eight separate CPU/allocation profiles pass exact
independent validation**, with no timeout, OOM or invalid result. Evidence was
preserved and verified in S3; all 34 owned services were inactive before release
at **2026-10-22T05:52:01Z**. The owner independently checked that evidence and
resumed its separate API scans at 05:52:30Z.

## Native C Arrow timings

Each query has three complete paired rounds. Every trial contributes to the
median and range. [Raw results](results.json), [complete group oracle](oracle.json),
and [derived summary](summary.json) retain all values and metrics. Reproduce the
summary with `python3 summarize.py results.json summary.json`.

| Query | Before median (range), seconds | After median (range), seconds |
|---|---|---|
| Q2 selective COUNT | 95.526 (95.429–98.443) | 0.0636 (0.0618–0.0750) |
| Q4 BIGINT AVG | 80.600 (80.385–81.568) | 78.149 (77.993–79.649) |
| Q9 grouped exact DISTINCT | 159.663 (158.872–162.372) | 96.586 (96.103–96.913) |
| Q10 grouped mixed aggregates | 280.192 (280.107–281.502) | 113.826 (113.448–114.018) |

Median elapsed reductions are Q2 99.93% (1,502×), Q4 3.04%, Q9 39.51%, and
Q10 59.38%. This is a matched comparison of four targeted aggregates, not an
overall ClickBench score or a ranking against another database product.

Q2 returns the exact count 630,500 in every trial. Its supported integer range
union enables indexed counting: median driver RESP traffic falls from 14.680 GB
to 10,295 bytes, while processed source rows fall from 99,997,497 to zero.
Diagnostics report the count during planning with no aggregate plan/worker grant.
The resulting Arrow row is fully fetched and independently validated.

Q4's elapsed gain is about 3%; client CPU falls from 941.990 to 472.506 CPU-seconds
and native execute allocation from 264.933 to 174.552 GB. Driver RESP traffic
remains about 24.926 GB and command volume remains about one per source row.
Typed inputs reduce client work while retaining authoritative BIGINT reads.

All three optimized Q9 trials select bounded client tuple compaction. Median
server CPU falls from 3,429.096 to 946.014 CPU-seconds, but client CPU rises from
445.139 to 1,004.460 and native allocation from 158.641 to 293.425 GB. Driver RESP
traffic rises from 5.882 to 27.209 GB. The largest observed process RSS rises from
4.432 to 5.993 GiB. Spill volume changes from 1.657 to 1.698 GB. These are explicit
costs of the faster path; it is not a client allocation or network reduction.

All three optimized Q10 trials also select bounded compaction. Median server CPU
falls from 4,061.202 to 1,005.377 CPU-seconds, while client CPU rises from 763.493
to 1,301.515 and native allocation from 270.430 to 381.469 GB. Driver RESP traffic
increases from 9.939 to 32.898 GB. Maximum process RSS changes from 5.109 to
5.625 GiB. The 16 GiB bound remains in force for both builds.

| Query | Returned tuples before→after, millions | Spill written/read before→after, GB | Final merge median before→after, seconds |
|---|---|---|---|
| Q9 | 21.928→45.657 | 1.657→1.698 | 11.119→11.482 |
| Q10 | 22.335→46.758 | 2.263→2.338 | 16.128→16.992 |

The 4096-entry compaction bound causes repeated flushes on the full fixture.
Returned tuple counts increase; spill bytes increase only about 2.5%/3.3% for
Q9/Q10. Every grouped trial represents all 99,997,497 source rows across 1,526
logical partitions, with zero row fallbacks or group-limit retries. Final merge
time is slightly higher; the substantial elapsed gain occurs before that merge.
Keep the complete native diagnostics when examining the CPU profiles.

The maximum sampled individual-primary RSS across all primary trials is
17.350 GiB. This includes the resident dataset and is not query-owned memory.
The raw report separately retains each trial's maximum sampled RSS and used
memory. Redis INFO totals and driver connection RESP counters have different
scopes and must not be added together as network volume.

## CPU and allocation profiles

[Eight raw profiles and logs](profiles/), [profile measurements](profile-analysis/profile-summary.json),
and [CPU/allocation top and cumulative reports](profile-analysis/) are retained.
Both grouped candidate profiles select the same compaction path as the six
native candidate trials. Render the reports with `analyze_profiles.py`, passing
the profiles directory, a new output directory, and the verified package directory.

| Query | Go query allocation before→after, GB | Allocations before→after, billions | GC cycles before→after |
|---|---|---|---|
| Q2 | 192.970→0.000233 | 2.701→0.000001567 | 5,785→0 |
| Q4 | 264.938→174.555 | 3.605→3.005 | 2,414→4,912 |
| Q9 | 158.643→293.749 | 2.676→5.104 | 1,457→819 |
| Q10 | 270.426→381.308 | 4.150→6.565 | 1,777→949 |

Q4's sampled process CPU falls from 931.91 to 473.77 CPU-seconds. Before, `decodeRow`
accounts for 329.82 cumulative sampled CPU-seconds and string-map lookup/hash
functions are prominent. `decodeRow` is absent from the candidate's sampled
profile; the direct typed scan replaces that path. These profiles support the
native allocation/CPU reduction without claiming a large elapsed reduction.

Q9/Q10 move more work into client RESP parsing, tuple construction and spilling.
`ReadReply` cumulative CPU is 69.73→209.79 seconds for Q9 and 123.20→261.62 for Q10.
Spill `flush` cumulative CPU rises 31.36→92.87 and 35.77→113.34 seconds, respectively.
Final reduction CPU changes less: 66.84→70.46 and 100.27→104.55 seconds. These
cumulative stacks overlap; they are not additive stages or elapsed durations.
Allocation profiles retain tuple-row map construction and RESP maps/slices as
large remaining costs. GC cycle counts alone do not establish GC CPU cost.

Planning and final Arrow output are small compared with source processing in
these one/ten-row aggregate results. Their precise CPU costs are not resolved by
the samples. The optimized Q2 profile contains only one 10 ms CPU sample, so its
function percentages cannot support stable attribution. Native timings, exact
outputs, command/RESP counters and allocation measurements provide its evidence.

## Identity and scope

This comparison isolates the #406 implementation merged in
[PR #428](https://github.com/alberttwong/redis-adbc-driver/pull/428). Baseline:
`e692a704d2b70428086e145df00e73db7b1a253f`; candidate:
`b40b88fe683780cdc17b05cb0e5c7e4108a42014`. Later retained-partition changes
in #430/#433 are outside these pinned binaries. The evidence branch includes
newer main changes without changing either measured binary.

The restored official fixture has **99,997,497 rows and 105 columns**. Source
Parquet SHA256:
`a390f6cb782f6aaef278c72fc1dd86c4f30bc843ebab3c159e9bd4d45ddb079f`.
Q2 and Q4 return one row; Q9 and Q10 return ten. Timings include each plan's
required source processing, execute, and complete Arrow fetch. They cover the
ordinary SQL statement API and its internal aggregate workers. Public ADBC
partition/retained-result API timings belong to the separate owner experiment.

Both Linux amd64 libraries were built with Go 1.26.6, GCC 12, identical CGO
flags and the driver version linker flag in the same pinned build image. SHA256:

| Binary | SHA256 |
|---|---|
| Before | `86f5324d2a50a9b93b2e60ff4d496e48f93dda0650ad6a59cd3238faf4af5309` |
| After | `7ac480ba91e0105cc7a7cb1c0eb2cb3d5339834d4c14e21edb94118412a0d610` |

The approved package SHA256 is
`62447612e4d24dec8ec8f0fcc5627ab48be942c50a67384528f7ddbea904ed62`.
Its 17 manifested files were verified after transfer. The timed runner comes
from `bd0c039`; the separate read-only settings capture comes from `7aee03f`.

## Matched environment and validation

The existing deployment has 48 Redis primaries and one dedicated client, all
c6a.4xlarge in the same availability zone: 784 allocated vCPUs and 1,568 GiB
host RAM in total. The client has 16 vCPUs and 32 GiB host RAM. Every isolated
trial has a 16 GiB cgroup limit, no swap, and 16 requested workers/budget tokens.
The supervisor has a 24 GiB cap. The independent DuckDB oracle uses four threads
and an 8 GB limit, and closes before measured queries begin.

All 48 primaries report Redis 8.6.2 and Search 8.6.0, a 24 GiB maxmemory setting,
`noeviction`, disabled automatic RDB/AOF persistence, and no preflight evictions.
The full allowlisted settings capture includes Search configuration for every
primary. Existing artifact upload continues; other benchmark query workloads
are paused during this reserved window.

The client uses Python 3.12.3, PyArrow 26.0.0, ADBC driver manager 1.12.0 and
DuckDB 2.0.0. redis-py 6.4.0 collects server telemetry. RESP3 is explicit for both
driver builds and the telemetry clients. See [settings](server-settings.json),
[package manifest](package-manifest.json), [build provenance](provenance.json),
and the [owner window identity](owner-window.json).

The dataset owner released the window at 2026-10-22T04:42:43Z after complete
backup and passing 48/48-primary restore verification. Source checksum, exact
row count and recovered topology are checked again by this harness. The
supervisor started around 04:49Z. The internal deadline is 06:50Z, before the
owner's fixed 06:55Z cutoff and 07:16:14.601018Z resource expiry.

The four unchanged official SQL queries each run three times per build in fresh
processes. Pair order reverses in the second round. Every trial is included;
there is no discarded warmup or best-of selection. An invalid result, timeout
or failed worker stops the matrix and remains in the raw report.

The independent oracle reads the complete Parquet file. Integer SUM uses
HUGEINT; AVG divides exact integer totals and counts with one final rounding to
double. Validation checks exact values, physical Arrow types, global rank and
permitted boundary ties against complete group universes. COUNT(DISTINCT)
remains globally exact across logical partitions. Full query text, actual
plans, worker grants and all outputs are retained.

Connection setup is outside query elapsed time. Reported client CPU covers the
fresh worker including connection/setup. Process peak RSS comes from getrusage;
transient service MemoryPeak may be unavailable after exit. The 16 GiB cgroup
limit is still enforced. Redis CPU/command/network deltas sum all primaries and
include monitor/control and cluster traffic. The memory sampler records the
largest individual primary, not total cluster memory.

Go Arrow CPU/allocation profiles run separately after primary native C Arrow
timings. Profile CPU includes process setup; logged query MemStats cover execute
and complete fetch. Cumulative allocation is distinct from peak memory. Worker
timers overlap: compact-tuple work includes synchronous spill callbacks, so
those durations must not be summed into elapsed time or a wall-time breakdown.

## Preservation

Every supervisor, isolation preflight, trial and profile service is journaled
before launch. Release requires all owned services to be inactive, no remaining
owned worker processes, and verified S3 preservation under
`full-c6a-20261021/performance-406/`. The existing instance role performs uploads;
credentials stay outside the package and evidence archives. Current HEAD checks
verify artifact size and SHA256 metadata and record each object version. The
report's current GET must return that same version, size and metadata, and its
downloaded bytes must match the recorded SHA256. Release settles only this #406
window; the owner then resumes its separate API measurements.

The [preservation report](preservation-report.json) lists all S3 artifact keys,
sizes, hashes and versions. The report object is 7,289 bytes, VersionId
`tDCCa1L8pv7tR3EyLz_NPnyotuNPluXy`, SHA256
`55fb0257d539181d8c4340d96f78f696a0a12fd6785e44553dd5aa875ec9811f`.
The downloaded 2,564,630-byte evidence archive also passed its recorded checksum.
See [inactivity audit](inactivity-audit.json), [completed supervisor](control.json),
and [release receipt](release.json). The four owned disposable local Redis test
containers were subsequently removed; cloud cleanup remains with the dataset owner.
