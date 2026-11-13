# Managed performance follow-up

Measured November 12, 2026. Exact unreleased native binaries:
base `f630e117201752b6ccf8c2dcb4da1d3e17203c3a` / `v0.0.26-dev.main-perf`;
candidate `62b0f6671922f6d8e699c38957db70cac41eb6c6` /
`v0.0.26-dev.managed-followup`. GetInfo/source/binary checks passed.

## Matched native reads

Median complete-result seconds for 100K rows × 105 columns. One reader reads
all rows; four readers process eight disjoint ordinal ranges. Six measured
repetitions after one warmup. Values, physical types and multiplicities are
independently checked outside timing for every complete output.

| Readers | Batch ceiling | Base | Candidate |
|---:|---:|---:|---:|
| 1 | 512 | 2.223 s | 2.279 s |
| 1 | 4096 | 2.234 s | 2.244 s |
| 4 | 512 | 1.859 s | 1.583 s |
| 4 | 4096 | 1.874 s | 1.559 s |

Worker RSS is the OS high-water mark: the maximum worker value in each trial,
then the median across its six measured trials. At the 4096-row batch ceiling:

| Readers | Base worker MiB | Candidate worker MiB |
|---:|---:|---:|
| 1 | 273.2 | 273.0 |
| 4 | 213.5 | 272.9 |

The larger bounded candidate pages trade higher worker resident memory for
fewer round trips. These values exclude the parent process and are not allocation
counts or a process-wide memory budget.

## Matched native writes and call-size control

Median seconds for 10K rows × 105 public columns plus ordinal. Existing-table
preparation is excluded; three measured trials after one warmup. A 1K-row call
ceiling reproduces the prior comparison. The separately labeled 10K Arrow
ceiling changes publication units: one writer makes one call instead of ten;
four writers each supply 2,500 rows, making four calls instead of twelve.
Internal 256-row/1 MiB mutation batches and 4096-row conversion slices remain
bounded. Larger-call improvements are an existing usage option, not a new
driver speedup or automatic grouping across calls. Each call publishes atomically.

| API | Call ceiling | Writers | Base | Candidate |
|---|---:|---:|---:|---:|
| SQL | 1000 | 1 | 7.851 s | 8.122 s |
| SQL | 1000 | 4 | 17.743 s | 18.127 s |
| ARROW | 1000 | 1 | 7.949 s | 7.888 s |
| ARROW | 1000 | 4 | 17.292 s | 17.630 s |
| ARROW | 10000 | 1 | 1.651 s | 1.621 s |
| ARROW | 10000 | 4 | 2.364 s | 2.339 s |

All 104 native trials, including warmups, completed and independently validated
with zero conflicts or automatic retries. All attempts are retained. These are
sequential base-then-candidate observations on a shared workstation, not confidence
intervals or guaranteed production improvements. Controls and observed ranges
are retained in the JSON; fixed-call comparisons are separate from call-size effects.

## Attribution diagnostics

The opt-in Go profile uses synthetic 100K × 105 BIGINT data with NULLs and values
above 2^53, persistent connections, complete Arrow consumption and validation
outside timing. It differs from the Python/C-ABI corpus and timing boundaries.
Three measured samples after warmup per case:

| Diagnostic | Base | Candidate |
|---|---:|---:|
| write call rows=1000 | 7.973 s | 8.648 s |
| write call rows=10000 | 1.817 s | 1.785 s |
| read readers=1 | 3.400 s | 3.456 s |
| read readers=4 | 3.382 s | 2.985 s |

The four-reader workload issued 392 candidate searches / 448 pipelines before
and 32 searches / 88 pipelines after; exact row fetch counts stay unchanged.
Command-time sums overlap across readers and are not wall time. Index readiness,
row identity, newest-version/tombstone and pin validation remain required.

Integer conversion microbenchmarks measure 10K values per operation, three
samples per revision. Median nanoseconds:

- base: 414716 ns; 96 B / 3 allocations per operation.
- candidate: 155590 ns; 96 B / 3 allocations per operation.

This CPU result does not establish the same speedup for complete managed writes.
Publication, index work and compaction admission remain substantial. The Go
fixed-1K-call write diagnostic was slower in this candidate run; that observation
is retained rather than presented as a write-throughput gain.

## Environment, timing and reproduction

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8; one RAM shard, 4 GiB,
noeviction; container four CPUs/eight GiB. Effective active 4× QPF, six Search
workers, mnp routing and 32 connections verified before cases and at completion.
Certificate-verified data TLS and RESP3. Apple M5 Max arm64, 18 logical CPUs/
36 GiB host RAM. Go 1.27.3; Python 3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0,
redis-py 8.1.0 and hiredis 3.5.0. No tests/profiles ran concurrently with native timing.

Native read timing includes fresh worker startup/imports, source-schema loading,
connections, complete fetch/conversion, IPC output and shutdown. Native write
includes fresh workers, source/key/literal construction, connections, complete
writes/publication and shutdown. Validation and existing-table setup are excluded.
Managed calls share table admission. These explicit range statements do not share
a cross-statement snapshot. Full input remains fixed during each read cohort.

Run `managed_followup.py` with private source/library/CLI/version/work paths,
`--dedicated-empty-server` and the [documented fixture environment](../README.md).
Defaults reproduce the six-read/three-write repetition matrix and larger-call control.
Run each native binary in a separate process with a fresh owned fixture/work directory.
GetInfo, source and binary hashes are retained in [measurements.json](measurements.json).
The inherited generic write-path declaration is preserved as `framework_write_paths`;
`write_paths` describes only measured managed paths. Trial observations are unchanged.

[summary.json](summary.json) contains recomputed distributions;
[diagnostics.json](diagnostics.json) contains all sanitized diagnostic samples/counts.
Run `python3 benchmarks/table-modes/managed_followup_audit.py` for native coverage
and arithmetic checks. See the
[validation record](../../../docs/sdlc/changes/managed-performance-followup/validation.md)
for test coverage, skips, review and cleanup. Operational identifiers, credentials,
raw logs/CPU profiles, licenses and the independently typed source remain private.
No 100M, physical Flex, Flight SQL, unmanaged/native-client comparison or creation
performance rerun is claimed here.
