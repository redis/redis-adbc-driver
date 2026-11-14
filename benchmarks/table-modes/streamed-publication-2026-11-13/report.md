# Explicit managed ingestion units — November 13, 2026

One explicit stream of 1K input batches substantially reduced elapsed time versus
separate 1K append calls in this local native workload, using exactly the same
driver. This is an existing API choice, not a new driver optimization or default.
The publication/failure boundary intentionally differs between those cases.

## Complete native results

Each cell has one retained warmup and three measured trials. All 24 complete
outputs passed independent physical-type/value/count/multiplicity and exact
ordinal validation, with no conflicts, retries, discarded samples or failed
measurement attempts. Input is 10K rows from the same independently typed
100K-row ×105-column, NULL-free mixed source, plus the ordinal.

| Writers | Input contract | Public calls | Input batches | Median s | Range s | Median maximum-worker RSS MiB |
|---:|---|---:|---:|---:|---|---:|
| 1 | Separate 1K calls | 10 | 10 | 7.656 | 7.624–7.772 | 256.0 |
| 1 | One stream, 1K batches | 1 | 10 | 1.848 | 1.833–1.868 | 253.5 |
| 1 | One 10K batch | 1 | 1 | 1.684 | 1.671–1.718 | 382.4 |
| 4 | Separate 1K calls | 12 | 12 | 16.209 | 16.077–16.224 | 247.8 |
| 4 | One stream per caller, 1K batches | 4 | 12 | 2.595 | 2.494–2.615 | 245.8 |
| 4 | One 2.5K batch per caller | 4 | 4 | 2.434 | 2.380–2.446 | 271.1 |

Four callers each ingest 2,500 disjoint rows; a 1K ceiling therefore produces
three batches/calls per caller, including its 500-row tail. Their managed
operations use the existing admission/serialization path; this is not parallel
managed publication. Caller count and input shape change protocol work and
publication count together. The experiment does not isolate Redis CPU, index
readiness, GC, C boundary or publication cost individually.

The one-stream medians are 75.9%/84.0% lower than separate-call medians for one/
four callers. Do not compound these with other changes or promise these ratios
on another workload. One large batch was faster still but had higher observed
RSS, especially for one caller. RSS includes source loading, Python, native/Go
buffers and worker lifetime; it is not isolated Go heap or summed process-tree
memory. All source and batch objects were materialized. A 1K input ceiling is
not evidence of bounded total application/operation memory.

## Source, boundaries and environment

All cases used clean driver `24f654b76b98f3d8960af94a3ca0230185d467a1`,
Makefile-built as `v0.1.0-dev.next-four-base` with Go 1.27.3. Both library/CLI
build identities and clean VCS metadata were verified. The independently
committed helper was `4491ae27`; its exact hash, source/binary hashes and fixture
configuration are in [provenance.json](provenance.json). Every sample and exact
summary remains in [measurements.json](measurements.json) and
[summary.json](summary.json).

The helper reuses existing connection/QPF/ownership gates and full-value
validators without changing established workloads. Timing includes spawned
worker startup/imports, complete source loading, combining chunks, preparing
input batches, connections, ingestion and teardown. Empty-table setup/reset and
independent output validation occur outside timing. The same process/timing
boundaries apply to all three shapes. Case order reverses between repetitions;
it is a short shared-workstation experiment, not randomized crossover or a
confidence interval. Other task builds/tests/measurements were paused; light
source/document work continued.

Owned Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808, one 4-GiB noeviction
RAM shard, active/effective 4x QPF, six Search workers and mnp/32 connections.
Four-CPU/eight-GiB container limits were verified. The same recorded local
container image was used throughout; its tag was not digest-pinned. Data TLS
requires certificate/hostname verification and RESP3. Management retains the
fixture bootstrap self-signed trust mode. Go native GOMAXPROCS is unset;
Apple M5 Max/18 logical CPUs/36 GiB, macOS 26.7.3 arm64. Python/package versions
are retained in observations. Operational identifiers/raw logs stay private.

## Reproduction and interpretation

Use [streamed_publication.py](../streamed_publication.py) with the exact source,
a versioned library/CLI, fresh private work directory and existing privately
configured `REDIS_BENCH_*`/`SSL_CERT_FILE` values. Provision an owned Software
fixture through `make -C go redis-up`, verify its effective settings/resource
limits and require `--dedicated-empty-server`. The helper clears only its owned
database and never retries input. Retain every failed attempt on a new run.

Run `python3 benchmarks/table-modes/streamed_publication_audit.py` to recompute
all six elapsed/RSS distributions and check exact cases, repetitions, call and
batch counts, source, QPF and complete validation. See the
[usage guidance](../../../docs/arrow-interoperability.md#choose-an-explicit-managed-ingestion-unit).
One managed stream stays private until its operation publishes; separate calls
can expose earlier successful publications. Unmanaged writes have per-row
partial effects. This does not transparently batch unrelated application calls
or demonstrate an input-error/unknown-reply recovery scenario; those are separate
driver contracts and tests.

The owned database was returned empty/index-free with all clients/processes
closed and effective QPF/TLS/RESP3 reverified. Final root teardown removed the
owned container/network and generated TLS/environment/settings/fixture-env
credentials, preserving the pre-existing container and 31 networks; see the
[sanitized proof](../current-main-next-2026-11-13/cleanup.json). Raw log hashes
are unchanged (zero generated-credential matches). No ClickBench or alternate
provider was used.
