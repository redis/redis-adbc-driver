# Single-pass append: bounded matched evidence

Date: 2026-11-20. Status: bounded correctness and matched control complete;
full-scale acceptance and human maintainer review remain pending.
See [intent](intent.md), [specification](spec.md), [plan](plan.md), and
[#564](https://github.com/redis/redis-adbc-driver/issues/564).

## Sources and method

Baseline production source: `1b5918e6`. Candidate production Go code matches
integration revision `fcb38aae`, including the single-pass append implementation
from `e460791e` and the bounded compaction change. The same instrumented
`go/snapshot_append_index_profile_test.go` was copied to both builds. Background
compaction was disabled in this control, so it does not measure the compaction
change or interactions with an active compactor.

The coordinating agent ran both static Linux ARM64 test binaries against the
same dedicated Redis Software Flex fixture (database reported Redis 8.8.0), from its Linux container network
namespace over TLS. `REDIS_SNAPSHOT_FLEX=true` enabled the driver's Flex stopgap.
Management checks established active QPF4, six Search workers, mnp routing,
32 connections, noeviction and one shard. Setup used `make -C go redis-up`.
Fixture identifiers, endpoints and raw operational logs remain private.

Each invocation loaded **16,384 total rows × 105 NOINDEX BIGINT columns** in one
long Arrow append per writer. Four writers received disjoint 4,096-row slices.
The source includes NULLs and exact integers above 2^53. The harness used a fresh
dedicated database for each observation. Setup, source construction and final
fetch/validation were outside acknowledged-publication timing. Every value,
NULL, logical ID, uniqueness and total row count was checked after full fetch.

There were three observations per revision and writer count, twelve total.
Within each writer count the order alternated paired baseline/candidate order:
serial B/C, C/B, B/C; four-writer C/B, B/C, C/B. No other heavy workload ran
during this clean window. An earlier matrix overlapped a Docker image pull and
was discarded in full; none of its observations appear below.

## Acknowledged publication

All times are milliseconds. Range is minimum–maximum across three observations.

| Writers | Baseline median | Baseline range | Candidate median | Candidate range |
|---|---:|---:|---:|---:|
| 1 | 1,754.746 | 1,714.944–1,843.535 | 1,461.918 | 1,453.818–1,477.739 |
| 4 | 1,432.557 | 1,404.004–1,486.465 | 1,131.011 | 1,111.303–1,149.376 |

In this bounded synthetic control the candidate median acknowledged time was
16.7% lower with one writer and 21.0% lower with four writers. These are observed
median differences, not confidence intervals or a canonical ClickBench result.
The baseline sent 64 final index-notification commands in every run (64 serial,
or 16 per parallel writer); the candidate sent zero. All twelve runs completed
their full result oracle successfully. Each stream still created one final index.

## Client observations and unavailable attribution

The matched instrumentation recorded client process CPU and peak resident memory
through acknowledgment. These are process observations for this control; they
do not identify server execution phases, core utilization or an ingestion CPU
bottleneck. Peak memory can include source construction before the timed append.

| Writers | Revision | Client CPU median (ms), range | Client peak RSS median (MiB), range |
|---|---|---:|---:|
| 1 | Baseline | 369.703, 366.014–395.285 | 294.613, 293.680–294.789 |
| 1 | Candidate | 346.825, 345.240–361.238 | 295.281, 293.402–300.516 |
| 4 | Baseline | 440.784, 440.472–462.110 | 558.359, 554.367–564.469 |
| 4 | Candidate | 429.151, 410.210–429.198 | 570.430, 566.656–572.340 |

Server CPU counters were unavailable; zero/absent proxy values are not evidence
of zero server CPU. SSD I/O and per-core utilization were not measured. Logged
native-info/proof and notification command durations overlap across writers and
include transport/setup work; they cannot be summed into independent server
phases. No server CPU, SSD I/O, server-phase speedup or optimal writer-count
claim follows from these logs.

## Correctness and remaining limits

The focused Go verification in [the plan](plan.md#evidence) passed on dedicated
Software RAM and Flex/QPF4 fixtures, including a race-enabled RAM concurrency
and failure subset. The coordinating agent additionally reported **156 Python
tests run successfully (153 passed and three unrelated isolation cases skipped)** on verified
Software Flex/QPF4, including serial, four-writer and four-writer NOINDEX paths.
See [combined verification](integration.md) for the final integrated Go checks.

This synthetic source has BIGINT columns rather than ClickBench's mixed types
and does not establish source Parquet conversion, string/TAG behavior, SSD
pressure, background-compaction backlog, reclamation lag or long-run memory
behavior. There was no 100-million-row ingestion, reusable full-scale backup,
AWS run or 43-query benchmark in this change. Existing input streaming remains
stopped. Full-scale issue acceptance and human maintainer review remain pending;
these results alone are not grounds to close the issue.

Product-version wording is clarified in the [measurement provenance erratum](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/provenance-erratum.md); historical timings and settings are unchanged.
