# Current-main reference — November 13, 2026

All **156 trials** independently validated: 84 reads, 40 existing-table writes
and 32 creation trials. These include 30 warmups and 126 measured observations.
No failed trials, serialization conflicts, caller retries or discarded samples.
This is a fresh reference at source `24f654b7`, not a matched comparison against
historical revisions or an attribution of any individual change.

## Complete result medians

`redis_py` denotes redis-py with hiredis.

Reads use 100K ×105 typed columns, six measured repetitions after a warmup
per case. Four readers execute eight SQL ranges.

| Mode | Readers | Arrow ceiling | Seconds | Maximum-worker RSS MiB | First batch s |
|---|---:|---:|---:|---:|---:|
| managed | 1 | 512 | 2.280 | 252.7 | 0.370 |
| managed | 1 | 4096 | 2.264 | 255.2 | 0.371 |
| managed | 4 | 512 | 1.609 | 253.4 | 0.500 |
| managed | 4 | 4096 | 1.556 | 255.4 | 0.504 |
| unmanaged | 1 | 512 | 5.223 | 255.9 | 0.385 |
| unmanaged | 1 | 4096 | 5.122 | 262.7 | 0.559 |
| unmanaged | 4 | 512 | 1.889 | 263.7 | 0.511 |
| unmanaged | 4 | 4096 | 1.794 | 266.0 | 0.516 |
| redis_py | 1 | 512 | 4.620 | 232.1 | 0.380 |
| redis_py | 1 | 4096 | 4.447 | 236.9 | 0.408 |
| redis_py | 4 | 512 | 2.114 | 255.9 | 0.523 |
| redis_py | 4 | 4096 | 2.197 | 279.6 | 0.567 |

## Existing-table writes

10K ×105 rows with up to 1K rows per public call/pipeline, one warmup plus three
measured observations per cell. Managed admitted operations serialize.

| Mode / API | Operation | Writers | Seconds | Maximum-worker RSS MiB |
|---|---|---:|---:|---:|
| managed / sql | append | 1 | 7.915 | 308.4 |
| managed / sql | append | 4 | 17.046 | 295.7 |
| unmanaged / sql | append | 1 | 2.562 | 301.0 |
| unmanaged / sql | append | 4 | 1.400 | 277.4 |
| managed / arrow | append | 1 | 7.915 | 256.4 |
| managed / arrow | append | 4 | 16.671 | 247.7 |
| unmanaged / arrow | append | 1 | 1.969 | 239.1 |
| unmanaged / arrow | append | 4 | 1.318 | 235.3 |
| redis_py / hset | append | 1 | 1.224 | 180.9 |
| redis_py / hset | append | 4 | 0.899 | 182.3 |

## Creation

10K ×105 rows with up to 1K rows per public call/pipeline, one warmup plus three
measured observations per cell. Managed admitted operations serialize.

| Mode / API | Operation | Writers | Seconds | Maximum-worker RSS MiB |
|---|---|---:|---:|---:|
| managed / arrow | create | 1 | 7.595 | 255.7 |
| unmanaged / arrow | create | 1 | 1.316 | 253.8 |
| managed / arrow | create_append | 1 | 7.657 | 256.4 |
| managed / arrow | create_append | 4 | 16.289 | 247.0 |
| unmanaged / arrow | create_append | 1 | 1.324 | 254.1 |
| unmanaged / arrow | create_append | 4 | 0.900 | 236.5 |
| redis_py / hset | create | 1 | 0.874 | 181.7 |
| redis_py / hset | create | 4 | 0.549 | 182.1 |

## Boundaries and environment

Driver clean source `24f654b76b98f3d8960af94a3ca0230185d467a1`, Makefile version
`v0.1.0-dev.next-four-base`, Go 1.27.3 with native GOMAXPROCS unset. Exact
library/CLI/source/harness hashes, limits and verified fixture settings are in
[provenance.json](provenance.json). Every sample/warmup/first-batch value and all
distributions remain in [measurements.json](measurements.json) and
[summary.json](summary.json). Python library versions are recorded in metadata.

Owned Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808: active/effective 4x QPF,
six Search workers, mnp/32 connections, one 4-GiB noeviction RAM shard, actual 4-CPU/8-GiB
container limits and certificate/hostname-verified data TLS/RESP3. Management
retains the existing self-signed bootstrap trust mode. Same fixed recorded local
container image throughout; its tag was not digest-pinned. Apple M5 Max, 18 logical
CPUs, 36 GiB, macOS 26.7.3 arm64. Operational identities and raw logs remain private.

All input loading/empty-table/index setup precedes read/existing-write timing.
Fresh spawned worker startup/imports, worker input loading/conversion, connections,
complete reads/writes, IPC output and shutdown are timed; independent canonical
physical-type/value/count/multiplicity validation is outside timing. Creation
includes table/index work, while catalog/schema initialization remains outside.
The read source is NULL-free mixed integer/string/date/timestamp/Boolean data;
these timings do not establish performance for every supported layout/type.

RSS is each trial’s maximum individual worker lifetime high-water mark, including
Python, source loading and native buffers, not summed application memory or Go
heap. Case order alternates within each suite; read→write→creation suites are
sequential. The separate 24-trial publication comparison preceded this reference.
Other task heavy work paused, but source/docs work continued on a shared host.
Small samples/order/cache/thermal effects limit inference; these are not confidence
intervals or Redis-only throughput measurements.

Driver unmanaged conditional-create writes protect whole-key absence; ordinary
redis-py+hiredis HSET does not. Existing-write fixtures use full field indexes;
unmanaged/raw creation uses membership-only indexes. SQL parse/literal construction,
Arrow conversion and source ownership differ between paths. The elapsed gap is
not entirely transport or conversion, and reader count is not managed write
parallelism. Refer to each row’s operation/index-strategy metadata.

## Reproduction and cleanup

The unchanged [arrow_compare.py](../arrow_compare.py) and shared
[run.py](../run.py) ran with read-repeats 6, write-repeats 3, write-rows 10000 and
sql-batch-rows 1000, suite all. Use an exact source/versioned build and a new private
work directory; privately configure REDIS_BENCH_* and SSL_CERT_FILE. Provision
an owned Software fixture using `make -C go redis-up` and verify effective settings
before execution. Retain every new attempt and its actual source/build identity.

Audit all cases and arithmetic with
`python3 benchmarks/table-modes/current_main_next_audit.py`. The owner returned
the database empty/index-free after all subprocesses exited and reverified
QPF/TLS/RESP3. Final owner teardown removed the owned container/network and
generated TLS/environment/settings/fixture-env credentials, preserving the
pre-existing container and all 31 pre-existing networks. The [sanitized proof](cleanup.json)
records completed cleanup. Private credential scans found zero matches, so raw
measurement hashes remain unchanged. No ClickBench or alternative server was used.
