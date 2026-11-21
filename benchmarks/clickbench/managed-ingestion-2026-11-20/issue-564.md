# Managed ingestion measurement: issue #564 closeout

Measured 2026-11-20 local time. Baseline `1b5918e6`; current `4faa8806`.
Issue: [#564](https://github.com/redis/redis-adbc-driver/issues/564).
[Method and hashes](method.json) · [Per-issue acceptance](acceptance.md) ·
[Product-version erratum](provenance-erratum.md).

## Result and scope

The eligible single-pass path removes the final index-notification pass in these
matched 105-column mixed canonical-data controls. Every observation passed exact
Arrow schema checks and independent, two-way DuckDB `EXCEPT ALL` validation of
all 105 values and row multiplicities. The results are bounded loading controls,
not a full-scale ClickBench score, a cold-SSD claim or a distributed-write result.
All public columns are explicitly NOINDEX; logical column types/nullability are
canonical. Unsupported layouts retain their separately tested deferred path.

## Exact transport-phase control

The same instrumented Go test source was compiled onto both driver revisions.
The 16,384-row Arrow IPC fixture contains 4,096 independently DuckDB-typed rows
from each of the first four canonical Parquet groups. IPC decoding/binding is
outside ACK timing. Background compaction is disabled only in this attribution
control. Three alternating observations per revision/writer count, fresh client
process each. Positive guards require bounded row writes, one index create per
worker, native proof/publication observations and the exact notification count.

| Revision | Writers | Median ACK seconds (range) | Median client CPU seconds | Max process RSS through ACK, MiB | Notify commands/run |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 1 | 3.358 (3.320–3.433) | 0.407 | 279.0 | 64 |
| baseline | 4 | 2.941 (2.885–2.947) | 0.513 | 563.1 | 64 |
| current | 1 | 1.140 (1.077–1.146) | 0.412 | 276.9 | 0 |
| current | 4 | 0.770 (0.763–0.792) | 0.493 | 603.4 | 0 |

[Raw sanitized control summaries](go-ipc-controls.json). Input/row-write windows
remain similar; the baseline's extra notification-command interval disappears.
Create, native readiness/membership proof and seal/publication hooks remain
positive. Per-worker command durations overlap and are not summed into wall-time
phases or called server CPU. Four-writer process RSS increased in this control;
these measurements do not establish a driver memory reduction.

## Streamed Parquet / production-maintenance control

The common current Python harness reads the first 65,536 raw rows of each first
four canonical row groups, rewritten into 64 groups of 4,096 rows: 262,144 rows,
all 105 columns. Values are unchanged; this derived fixture is not the first
262,144-row global source prefix and does not represent original large-group
reader memory. Source conversion, options and limits are identical across driver
versions. Each worker supplies one long append. Ordinary maintenance, four-segment
trigger and eight-segment admission limits remain enabled and unchanged.

Three alternating observations for each revision/writer count, fresh client
process each; reused Redis process, allocator and Flash caches. Owned row/index/
directory and metadata/data/compaction queues drain before the next case.

| Revision | Writers | Median ACK seconds | Median post-ACK compaction-quiet observation, seconds | Median client process CPU, seconds | Max process RSS through ACK, MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 1 | 55.303 | 2.005 | 9.043 | 435.9 |
| baseline | 4 | 47.956 | 98.178 | 10.268 | 1021.6 |
| current | 1 | 20.061 | 2.007 | 7.461 | 436.1 |
| current | 4 | 13.452 | 71.139 | 8.163 | 1016.2 |

[All twelve sanitized observations](cshared-controls.json) also record independent
validation/export/oracle and cleanup times. ACK means published/readable data;
compaction quiet is a separate observation, not an established prerequisite for
queries or a compatible backup. No backup was created or certified. Fixed polling
adds about two seconds to the no-job quiet observation. Full-result validation
and reclamation are outside ACK time and must not be omitted from session costs.

## Server CPU and physical I/O

These are medians of sampled inner/enclosing load-window counters, not confidence
intervals or exact command-phase integrals. The server observer samples at 1s;
client phase/resource samples at 0.25s. Enclosing windows can include setup or
early maintenance, so their durations are retained with each observation.

| Revision | Writers | Data Redis CPU seconds, inner–enclosing | Software-container physical reads GiB, inner–enclosing | Software-container physical writes GiB, inner–enclosing |
| --- | ---: | ---: | ---: | ---: |
| baseline | 1 | 234.41–237.15 | 4.239–4.270 | 1.928–1.966 |
| baseline | 4 | 247.59–251.01 | 4.623–4.639 | 1.909–1.962 |
| current | 1 | 14.27–15.22 | 1.082–1.082 | 0.504–0.504 |
| current | 4 | 14.41–15.78 | 1.467–1.468 | 0.707–0.714 |

Data Redis is selected by its observed bigstore worker threads; PID/start-time
identity is checked before differences. Its main thread is identified by TID=PID;
other named threads are not speculatively equated to Search-only CPU. Whole-container
cgroup I/O has one device entry and includes management/control activity. Per-PID
`/proc/io` is permission-unavailable; all-zero proxy INFO CPU is unavailable,
not measured zero. Direct kernel process CPU and container I/O remain available.
Client CPU includes its runtime, conversion and observers. No RSS-minus-Arrow
subtraction is presented as Go heap; GC traces and Arrow/cgroup counters are
separate measurements.

## Correctness, lifecycle and acceptance

Prior focused [native/fault verification](../../../docs/sdlc/changes/issue-564-single-pass/plan.md)
covered Software RAM/Flex with verified QPF4, parallel append/rebase, pinned roots,
abort, creation/readiness failures, permission handling and uncertain outcomes.
This measurement adds canonical mixed values, serial/four-writer controls and
actual resource observations. Unknown outcomes are preserved without replay or
optional DDL cleanup. A Redis-free resource watchdog enforces positive initial
samples, PID incarnation/freshness, memory and disk headroom and cancellation
ownership. Five pure instrumentation checks passed before confirmatory runs.

Issue #564 does not require a 100-million-row run. Its bounded representative
comparison is supported here; #566's explicit full-canonical ingestion criterion
remains separate. Findings do not promise a full-scale speedup, lower peak RAM,
all-core saturation, production NVMe throughput or backup/restore compatibility.
