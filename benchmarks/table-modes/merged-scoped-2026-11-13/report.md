# Merged-main table-mode comparison — November 13, 2026

All **156 trials** independently validated: 84 reads, 40 existing-table writes and 32 creation trials. These include 30 warmups and 126 measured observations. No failures, serialization conflicts, caller retries or discarded samples. This fresh reference measures clean merged source `ca6858065c4cf02418d55c3aa62eb583590fe252` after PRs #433, #434 and #435. It does not attribute a gain to an individual change or compare revisions under matched conditions.

## Reads

100K rows ×105 typed columns; six measured repetitions after a warmup per case. Four readers execute eight disjoint SQL ranges; no shared cross-statement snapshot. `redis_py` denotes redis-py with hiredis.

| Mode | Readers | Arrow ceiling | Median seconds | Max-worker RSS MiB | First batch s |
|---|---:|---:|---:|---:|---:|
| managed | 1 | 512 | 2.348 | 253.3 | 0.376 |
| managed | 1 | 4096 | 2.268 | 253.7 | 0.369 |
| managed | 4 | 512 | 1.504 | 253.4 | 0.485 |
| managed | 4 | 4096 | 1.495 | 256.0 | 0.483 |
| unmanaged | 1 | 512 | 5.056 | 257.0 | 0.382 |
| unmanaged | 1 | 4096 | 4.894 | 263.6 | 0.550 |
| unmanaged | 4 | 512 | 1.823 | 263.1 | 0.494 |
| unmanaged | 4 | 4096 | 1.667 | 266.2 | 0.478 |
| redis_py | 1 | 512 | 4.514 | 232.1 | 0.378 |
| redis_py | 1 | 4096 | 4.370 | 235.9 | 0.405 |
| redis_py | 4 | 512 | 2.138 | 253.6 | 0.513 |
| redis_py | 4 | 4096 | 2.128 | 251.3 | 0.544 |

## Existing-table writes

10K total rows ×105 columns plus ordinal, divided across callers; up to 1K rows per public call/pipeline. One warmup and three measured repetitions per case. Managed admitted operations serialize; four callers do not imply four simultaneous managed mutations.

| Mode / API | Operation | Callers | Median seconds | Max-worker RSS MiB |
|---|---|---:|---:|---:|
| managed / sql | append | 1 | 8.113 | 309.8 |
| managed / sql | append | 4 | 19.286 | 286.7 |
| unmanaged / sql | append | 1 | 2.550 | 295.5 |
| unmanaged / sql | append | 4 | 1.346 | 276.5 |
| managed / arrow | append | 1 | 8.330 | 256.6 |
| managed / arrow | append | 4 | 17.170 | 248.3 |
| unmanaged / arrow | append | 1 | 2.072 | 239.3 |
| unmanaged / arrow | append | 4 | 1.288 | 230.2 |
| redis_py / hset | append | 1 | 1.207 | 181.7 |
| redis_py / hset | append | 4 | 0.923 | 182.1 |

## Creation

10K total rows ×105 columns plus ordinal, divided across callers; up to 1K rows per public call/pipeline. One warmup and three measured repetitions per case. Managed admitted operations serialize; four callers do not imply four simultaneous managed mutations.

| Mode / API | Operation | Callers | Median seconds | Max-worker RSS MiB |
|---|---|---:|---:|---:|
| managed / arrow | create | 1 | 7.605 | 255.6 |
| unmanaged / arrow | create | 1 | 1.309 | 253.4 |
| managed / arrow | create_append | 1 | 7.400 | 255.7 |
| managed / arrow | create_append | 4 | 16.120 | 248.3 |
| unmanaged / arrow | create_append | 1 | 1.292 | 254.0 |
| unmanaged / arrow | create_append | 4 | 0.829 | 236.6 |
| redis_py / hset | create | 1 | 0.874 | 181.8 |
| redis_py / hset | create | 4 | 0.514 | 182.2 |

## Environment and timing boundaries

Run date uses America/Los_Angeles. Clean source `ca6858065c4cf02418d55c3aa62eb583590fe252`; Makefile build version `v0.1.0-dev.merged-scoped-1`, Go 1.27.3, native GOMAXPROCS unset. GOGC/GOMEMLIMIT/GODEBUG: {'GOGC': 'unset', 'GOMEMLIMIT': 'unset', 'GODEBUG': 'unset'}. Exact binary, input and harness hashes are in [provenance.json](provenance.json). Full samples, warmups and distributions are in [measurements.json](measurements.json) and [summary.json](summary.json). Python 3.15.2; Python package versions are recorded in measurement metadata.

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808. Active/effective 4× QPF, six Search workers, mnp routing/32 connections, one 4-GiB noeviction RAM shard. Actual container limits: 4 CPUs/8 GiB. Data connections use strict certificate/hostname-verified TLS and RESP3. Management retains the existing self-signed bootstrap trust mode. One fixed recorded local container image throughout; its tag was not registry-digest pinned. Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Operational identities and raw logs remain private.

Input loading and table/index setup are excluded from read and existing-write timing. Timed scopes include fresh spawned worker startup/imports, worker source loading/conversion, connections, full reads/writes, IPC and shutdown. Creation includes table/index work; schema/catalog initialization remains outside. Independent canonical physical-type/value/count/multiplicity and ordinal validation runs outside timing. The read input is NULL-free mixed integer/string/date/timestamp/Boolean data; it does not measure every supported type/layout.

Managed statement snapshots and unmanaged live HASHes have different guarantees. Unmanaged conditional-create protects whole-key absence; ordinary HSET does not. Existing-write fixtures have full field indexes; unmanaged/raw creation has membership-only indexes. SQL parsing/literal construction, Arrow conversion and ownership differ. The elapsed gap cannot be attributed entirely to transport or conversion.

RSS is the maximum individual worker lifetime high-water mark including Python/source/native buffers, not summed application memory or Go heap. Case order alternates within each suite; read→write→creation suites run sequentially. Other heavy work and slide rendering were paused during timing; light source/docs/API work continued on a shared host. Small samples, ordering, caching and thermal effects limit inference; distributions are not confidence intervals.

## Reproduction and cleanup

Run unchanged [arrow_compare.py](../arrow_compare.py) with shared [run.py](../run.py), suite all, read-repeats 6, write-repeats 3, write-rows 10000 and sql-batch-rows 1000. Provision an owned Software fixture via `make -C go redis-up`, verify effective settings, build at the exact clean source/version, privately configure REDIS_BENCH_* and SSL_CERT_FILE and retain every attempt.

Audit exact cases, repetitions, validations, settings and arithmetic with `python3 benchmarks/table-modes/merged_scoped_audit.py`. The database was returned empty/index-free after all subprocesses exited and QPF/TLS/RESP3 reverified. Owned container/network and generated TLS/environment/credential files were removed, preserving preexisting resources; [cleanup.json](cleanup.json) records the completed owner teardown. No ClickBench, Flex or Flight SQL benchmark ran.

The Google Slides refresh selects 4096-row read medians and append medians; all creation modes and both read ceilings remain here. Supporting v0.0.25 and historical 100M slides retain their actual build/server/settings.
