# Compaction reply wait: evidence only

No production change is justified by this experiment. The new untouched dense
control reproduced the earlier wait: 314 copy releases took **6.254 seconds**,
including 6.247 seconds in TLS Read, within an 8.165-second job. Adding monitor
requests moved much of the measured wait into the request preceding release.
The server commandstats snapshots did not support per-release attribution.
Neither Lua execution time, server queuing, Search work nor a transport mechanism
was established as the cause. All production Go files, modules and default tests
remain unchanged from `88774f10`.

## Complete attempts and correctness

| Attempt | Frozen diagnostic source | Go exit | Release observations | Attribution result |
|---|---|---:|---:|---|
| Unchanged dense I/O control | `549cbd4a` | 0 | 317 | Valid client timing control; no commandstats monitor |
| Initial INFO monitor | `549cbd4a` | 0 | 634 | Invalid: all 634 commandstats flags false; observer failures changed release outcomes and triggered existing bounded cleanup retries |
| Corrected record-only monitor | `d2abe735` | 1 | 317 | Invalid: strict final assertion rejected all 317 per-release deltas after complete job/output validation |

Every job independently fetched and checked all 20K exact BIGINT/payload target
rows. It separately checked the unrelated table's COUNT/MIN(id)/MAX(id) as
20,000 / 1 / 20,000; that aggregate check is not a full unrelated-table fetch. The first Go exit0 did **not** establish a valid
attribution measurement: a subsequent full-sample audit found its observer errors.
The correction preserves the original release outcome, records fixed before/after
counter totals and request durations, and asserts diagnostic validity only after
normal releases and complete result validation. It did not alter production
release code or remove any cleanup/proof/lease operation. No attempt was discarded.

Focused parser race tests passed 17 tests including subtests on `549cbd4a`;
the corrected parser/outcome regression race run passed 18 on `d2abe735`. Both had
zero failures/skips. The endpoint-free regression proves a diagnostic counter
failure does not change the original release result or issue a second release.
The corrected live test intentionally failed its stricter diagnostic assertion;
it is not counted as a passing test or valid server execution measurement.

## Where the observer measured the wait

These are all 317 phase releases (314 copy, one index, two install), except for
the separately identified copy-only total above. Read time is contained in hook
time; neither should be added together or added again to whole-job elapsed.

| Measurement | Unchanged control | Corrected INFO monitor |
|---|---:|---:|
| Whole job elapsed | 8.164897 s | 7.059629 s |
| Release hook median | 20.091 ms | 0.291917 ms |
| Release hook total | 6.296072 s | 0.096386 s |
| Release TLS Read total | 6.288910 s | 0.087266 s |
| Before-INFO median | absent | 18.042959 ms |
| Before-INFO total | absent | 5.079655 s |
| After-INFO median | absent | 0.251792 ms |
| After-INFO total | absent | 0.081968 s |

INFO requests used a separate certificate-verified TLS client and are outside the
release hook/socket timer, but inside step/job timing. Each corrected release
succeeded with one Read/Write and zero pool misses/timeouts. Its INFO-before wait
range was 0.245–22.512 ms, while release hook duration was 0.198–0.612 ms.
The added requests changed pacing, response sizes, transport use and server work.
These are separately labeled diagnostic jobs, not matched throughput candidates;
the lower whole-job elapsed is not a production speedup. The corrected run followed
another serialized benchmark block on the same workstation/server. This does not
prove host idleness or eliminate ordering, cache or instrumentation effects.

The corrected INFO pairs produced **316 all-zero script deltas and one jump of
20 calls / 20,107 microseconds**, despite exactly one actual release inside each
pair. Zero pairs cannot establish zero execution time, and the multi-call pair
cannot be assigned to one release. All 317 fail the predeclared exact-one-call
visibility gate. Counter aggregation, staleness, routing and other mechanisms
were not distinguished. No per-release CPU fraction can be calculated from this
experiment, and the remaining client time cannot be labeled network-only.

## Environment, provenance and reproduction

Redis Software API 8.2.6-25, Redis 8.8.0, Search 80808; active/effective 4x QPF,
six Search workers, mnp routing / 32 connections, 4 GiB noeviction RAM shard.
The owner provisioned through `make -C go redis-up`, verified actual four-CPU /
eight-GiB container limits and used one explicitly recorded local image identity.
Strict data TLS certificate/hostname verification and RESP3 remained enabled.
Host Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Go 1.27.3,
GOMAXPROCS=4, packages sequential and test caching disabled. Focused tests used
race instrumentation; latency profiles did not. Operational identities, TLS
material, credentials and raw logs remain private.

[results.json](../../../docs/sdlc/changes/compaction-reply-attribution/results.json)
retains every sample, phase/count data, sanitized environment, command outcomes
and private raw-log hashes. [patches.json](patches.json) identifies the two exact
unapplied diagnostic/test diffs; `codex/compaction-reply-attribution` retains the
corrected frozen source. The original-to-corrected source chain includes
`549cbd4a` → `9ab8c19f` → `5145ce84` → `d2abe735`; only the first and last were
executed. See [reproduce.md](reproduce.md) for original commands and the expected
unsupported per-release measurement outcome. The corrected diagnostics are an
archived experiment, not an advertised working metrics feature.

Audit all attempts without Redis or Go:

```sh
python3 benchmarks/table-modes/compaction_reply_audit.py
```

Each exclusive loan ended with all processes/clients closed, the database empty
and index-free, and effective QPF/TLS/RESP3 reverified. The parent completed final
shared teardown: the owned container/network and generated TLS/environment/settings/
fixture-env credentials were removed; the pre-existing container and all 31
pre-existing networks were preserved. [Sanitized cleanup proof](cleanup.json)
is included. The owner's exact generated-credential scan found zero matches,
so raw log hashes remain unchanged. No ClickBench was run.
