# Restored ClickBench prefetch screen, 2026-11-03 (UTC run: November 4)

Disposition: retain `scanprefetch` as an experimental build tag, disabled by default. The one-reader wide cases improved repeatedly; four-reader ranges overlap, and the narrow regular four-reader median regressed 1.77%. These results do not justify general default enablement.

## Method and identity

Baseline driver: `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`. Measured candidate runtime and Go harness: `87a48c510efaeab14f45243e6291079db78e8603`, tag `scanprefetch`. Both Linux amd64 Go 1.27.2, `-ldflags "-s -w"`, 16 client logical CPUs, same server and fixture. Binary SHA-256 values and independent source identity are in [summary.json](summary.json). The runner in this PR includes the SELECT-nullability validation correction described below.

**Revision limit:** after these measurements, a shutdown-only fix makes connection Close join initial-fill transports and makes the worker finish cursor cleanup before signaling completion. Its regression test and full race suite pass. The final PR revision has not been rebenchmarked; do not relabel these numbers as measurements of that revision. Connection Close is outside the inner timer, but worker finalization can intersect terminal stream cleanup. No default promotion is claimed.

Public Go ADBC APIs and Go Arrow allocator; execute/partition planning, complete fetch, IPC output and stream/statement cleanup are timed. Connections are preopened; independent validation and database/connection teardown are excluded. This is not Python/C shared-library performance. Each of eight cases has one warm-up per build plus five measured alternating-order trials per build (96 executions). Four public partition descriptors are consumed by one or four readers. Four regular readers consume disjoint exact ID ranges. Every reader was active; every execution returned exactly 100,000 rows and passed independent full-value, type, NULL and multiplicity validation.

Redis Software 8.6.2, Search 8.6.11, RESP3, verified TLS, 48 shards, active verified **4x QPF** (six Search workers, mnp, 32 connections) checked before every execution. Same-AZ 48 server hosts plus one client, each c6a.4xlarge (16 vCPU, 32 GiB); server container limit 29 GiB, database memory 900 GiB, no eviction, persistence disabled. These resources are large relative to the derived fixture; no production sizing claim follows.

All 48 full-scale snapshot SHA-256 values were checked before restoring 99,997,497 rows on Software. Historical snapshot provenance is Community; no Community benchmark was run. The complete restored table was preserved. A separate managed 100K fixture was prepared with pinned official DDL and SQL INSERT SELECT of exact internal IDs 1..100000 outside timing. No Parquet ingestion or full-scale reload occurred. This permits predicate-free public partition reads; a predicate on the full table deliberately selects its existing residual fallback.

The independent reference joins native exact row-ID/unique WatchID membership to the checksum-verified official Parquet, using the pinned PostgreSQL DDL and tracked typed conversion helpers. All 105 expected values come from the official source, not either driver. Parallel loading means these rows are not necessarily the Parquet prefix. See [source-proof.json](source-proof.json), [fixture-preparation.json](fixture-preparation.json) and [selection-prefetch.json](selection-prefetch.json). Narrow projection has three fields.

## Direct complete-fetch results

Seconds: median [minimum, maximum]; negative change is faster. Wins count candidate faster than baseline in each of five paired rounds. Warm-ups are excluded.

| Projection/API/readers | Baseline seconds | Candidate seconds | Change | Wins |
|---|---:|---:|---:|---:|
| narrow / partition / 1 | 2.040 [2.029, 2.065] | 2.031 [2.017, 2.086] | -0.46% | 3/5 |
| narrow / partition / 4 | 0.797 [0.796, 0.876] | 0.778 [0.763, 0.806] | -2.36% | 4/5 |
| narrow / regular / 1 | 2.452 [2.429, 2.490] | 2.447 [2.416, 2.483] | -0.22% | 3/5 |
| narrow / regular / 4 | 1.257 [1.222, 1.316] | 1.279 [1.213, 1.296] | +1.77% | 2/5 |
| wide / partition / 1 | 8.298 [8.222, 8.430] | 7.020 [6.921, 7.074] | -15.41% | 5/5 |
| wide / partition / 4 | 2.977 [2.838, 3.178] | 2.859 [2.714, 3.080] | -3.96% | 3/5 |
| wide / regular / 1 | 9.022 [8.939, 9.050] | 7.805 [7.712, 7.870] | -13.50% | 5/5 |
| wide / regular / 4 | 3.717 [3.702, 3.832] | 3.439 [3.314, 3.878] | -7.48% | 4/5 |

Both one-reader wide cases won all five pairs with nonoverlapping ranges. Four-reader wide results overlap and include an ordinary-reader outlier. Narrow does not select prefetch; retain its observed regression rather than attributing every small difference to the prototype.

## Client costs and batch sizes

Each cell is baseline → candidate median. Allocated bytes are cumulative Go allocations in the measured interval. Heap is a 20 ms sampled live Go peak, not a strict peak. RSS is process-lifetime high-water and can reflect prior runtime history; the repeated identical RSS values cannot establish per-case resident-memory differences. Units MB are decimal.

| Projection/API/readers | CPU seconds | Allocated MB | Allocation count (millions) | Sampled heap MB | Lifetime RSS MB | First batch rows |
|---|---:|---:|---:|---:|---:|---:|
| narrow / partition / 1 | 0.759 → 0.745 | 236.4 → 236.3 | 3.965 → 3.965 | 25.8 → 26.2 | 431.2 → 431.2 | [25000] → [25000] |
| narrow / partition / 4 | 0.662 → 0.663 | 236.1 → 236.1 | 3.965 → 3.965 | 57.6 → 55.8 | 431.2 → 431.2 | [25000] → [25000] |
| narrow / regular / 1 | 0.817 → 0.802 | 231.6 → 231.5 | 4.282 → 4.282 | 29.4 → 27.5 | 431.2 → 431.2 | [65536] → [65536] |
| narrow / regular / 4 | 0.753 → 0.776 | 240.4 → 240.4 | 4.441 → 4.441 | 55.6 → 62.1 | 431.2 → 431.2 | [25000] → [25000] |
| wide / partition / 1 | 4.743 → 4.778 | 1174.3 → 1174.5 | 32.745 → 32.745 | 113.4 → 95.4 | 431.2 → 431.2 | [25000] → [25000] |
| wide / partition / 4 | 4.608 → 4.755 | 1174.2 → 1174.3 | 32.745 → 32.745 | 310.6 → 307.7 | 431.2 → 431.2 | [25000] → [25000] |
| wide / regular / 1 | 4.391 → 4.403 | 1161.7 → 1161.8 | 33.041 → 33.041 | 160.4 → 161.4 | 431.2 → 431.2 | [65536] → [65536] |
| wide / regular / 4 | 4.669 → 4.709 | 1178.1 → 1178.2 | 33.219 → 33.219 | 276.2 → 271.8 | 431.2 → 431.2 | [25000] → [25000] |

The additional-page synthetic retained-heap diagnostic is approximately 28.44 MB for 4,096 × 105 unique roughly 32-byte strings (Go object/slice overhead included). It is not a strict byte cap or a measured ClickBench page size. Four readers can retain four extra pages; arbitrary variable-width HASHes increase memory. No allocation reduction is used as a promotion argument.

## Both wire directions (separate diagnostic)

A TLS-transparent loopback relay counts actual TCP payload in each direction, including TLS setup/framing but excluding IP headers and separate INFO control traffic. This is a separate smoke diagnostic: one warm-up and one measured execution per build/case (32 executions), all independently validated. Proxy elapsed values are excluded from the primary result and provide no performance gate. MB below are decimal measured payload. Server endpoint INFO CPU deltas were zero, so no server CPU decomposition is inferred.

| Projection/API/readers | Request MB baseline → candidate | Reply MB baseline → candidate |
|---|---:|---:|
| narrow / partition / 1 | 8.729 → 8.729 | 22.247 → 22.247 |
| narrow / partition / 4 | 8.737 → 8.737 | 22.258 → 22.258 |
| narrow / regular / 1 | 8.714 → 8.714 | 24.095 → 24.095 |
| narrow / regular / 4 | 8.739 → 8.739 | 25.066 → 25.064 |
| wide / partition / 1 | 193.480 → 193.480 | 151.845 → 151.846 |
| wide / partition / 4 | 193.488 → 193.488 | 151.857 → 151.859 |
| wide / regular / 1 | 193.464 → 193.464 | 153.689 → 153.692 |
| wide / regular / 4 | 193.490 → 193.490 | 154.712 → 154.713 |

No server payload format changed. Similar byte counts are expected; this prototype overlaps existing discovery/HMGET work and Arrow conversion.

## Failures, evidence and limits

The initial restore pilot selected a data shard without the driver catalog; the checksum-verified catalog-owning shard passed a fresh 20-row × 105-field independent compatibility check before expansion. The first full-table predicate selection probe correctly rejected public partition prefetch eligibility; the small derived fixture resolved that without changing fallback behavior. Neither setup attempt supplies a timing result.

The first derived-fixture baseline warm-up failed schema equality: all 105 source DDL fields were non-nullable, while SELECT outputs are nullable by the existing driver contract. Diagnosis found 100,000 rows, no name/type differences, and no column value or validity differences. The validator now models SELECT field nullability separately while retaining exact types, values and every NULL bit; a regression test rejects a changed NULL bit. The failed warm-up and diagnosis are retained, and a fresh output directory contains the accepted full matrix. See [first-warmup-comparison-diagnosis.json](first-warmup-comparison-diagnosis.json). Local initial sandbox-listener refusals and an externally stopped shared fixture are documented in the change validation record.

Machine-readable [direct-results.json](direct-results.json), [wire-results.json](wire-results.json) and [summary.json](summary.json) retain every trial, warm-up, first-batch latency/size, CPU/allocation/memory sample summary and QPF proof. Raw Arrow IPC, failed outputs, source reference and full operational logs are archived privately with checksums; infrastructure identifiers, endpoints, credentials and licenses are intentionally absent here. Reproduce with the [runner instructions](../README.md) and the exact source/binary identities above.

The typed server export investigation has a separate [contract and engineering no-go](../../../docs/typed-export-design.md). These timings do not measure native typed export, prove a server-owner approval, or establish a new server capability.

## Resource cleanup

[Sanitized cleanup verification](cleanup.json): all 49 task-owned instances are
terminated; no active/attached root volumes or network interfaces remain. Task
security group, SSH key, cleanup rule/function and task log group were removed.
Five unattached root disks still report AWS asynchronous `deleting` state;
deletion has been accepted, but physical disappearance is not claimed. The first
zero-volume wait timed out and is retained privately; final verification separates
pending AWS deletion from active resources. Existing backups and shared IAM
resources were preserved. Completion was within the approved eight-hour deadline.
The conservative compute-only upper estimate is $46.01, not a final invoice;
storage/network charges are additional. The approved total cap was $300.
