# Sustained-write diagnostics and bounded compaction reads

Date: 2026-11-04. Parent: 9fbbaa4. Status: focused regression matrix and both-order native comparisons completed; performance parity failed for this workload.
PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Workload and initial evidence

The native benchmark now has `--sustained-writes --write-count 64`: two tables,
20,000 rows each in this run, alternating point UPDATEs with complete RETURNING
fetch. Each UPDATE increments an exact BIGINT above 2^53. Every returned value
is checked; after each interval all 40,000 rows, IDs, values and string payloads
are independently calculated and compared. One warmup update per table precedes
measurement. There are no caller retries; an error or refusal fails the run.
Setup and final complete verification are outside the interval timer. Reports
retain operation order, every latency, interval throughput, median, nearest-rank
p95 and maximum. This is a finite single-client workload, not proof of indefinite
compaction stability or multi-writer scalability.

Driver 9fbbaa4 (native library SHA-256
`f607dda72f983fd86db72ca38fe7043eb634a9b2cb68ce8a71ac4bd521a30551`)
completed 64 updates and complete final validation without retries. The interval
took 147.879 seconds: 0.433 updates/second, median 1,730.333 ms, p95 6,586.182 ms,
maximum 6,798.460 ms. A read-only durable-checkpoint probe occurred during this
run to diagnose its long runtime. Therefore it is diagnostic evidence only,
not an isolated comparative performance sample. Its checkpoint was in the copy
phase, advancing through row IDs without a recorded worker error.

The fixture was strict-TLS Redis Software, Redis 8.8.0, three primary shards
behind its Software proxy, verified active/effective 4x QPF and noeviction.
Python 3.13.16, PyArrow 26.0.0, ADBC manager 1.13.0 and redis-py 8.1.0;
macOS ARM64, Go 1.27.3, native library built with `make -C go build` version flags.
These settings differ from older published Redis 8.6.2 cohorts. No Community,
ClickBench, Cluster API or failover result is inferred.

`TestSQLRekeyConcurrentWriters` also passed on the unchanged parent in 3.700 s
(root 5.428 s), with its existing writer-progress deadline intact. This did not
reproduce or resolve the historical hosted stall.

## Directory read overhead

Compaction reconstructs a visibility scanner at every 64-row checkpoint. Its
source head used the general 256-entry page, discarding unused entries after
that step. A real four-segment, 500-row fixture reproduced requests for 256
entries per source despite the 64-row checkpoint bound. The scanner now takes
the smaller of its normal page and explicit batch limit, and uses that same
page size for end-of-directory/cardinality checks. Default query paging remains
256; compaction still resolves winning rows/tombstones before copying.
No quota, lease, checkpoint, publication or index-readiness fence changes.

The regression checks all copy checkpoints advance, the output directory has
499 visible rows, and completed compaction preserves the deletion and exact
BIGINT replacements. Before the fix it failed with the observed 256-entry
request. Afterward it passed with the 64-entry limit. The existing 100,001-row
bounded-compaction test also passed (51.310 seconds). A first invocation omitted
the snapshot endpoint and skipped the new case; only the subsequent explicitly
enabled failure/pass runs count as evidence.

The final related race selection passed 35 test events with zero test skips or
failures; root package 95.484 seconds. It selected
`TestSnapshot(Compaction|BackgroundCompaction|Delta|Partition|Statistics)|TestSQLRekeyConcurrentWriters`
using `make -C go test-software` with `-race -json -failfast` and an explicitly
set disposable snapshot endpoint. This includes directory damage, pinned streams,
checkpoint takeover, byte bounds, queue fairness and authoritative statistics
cases. The CUDA support package had no applicable tests. `git diff --check`
passed. The final versioned `make -C go build` also passed. [Raw diagnostic samples](sustained-write-diagnostic.json) are retained
with their observer-probe qualification. Fresh matched-source timings are recorded below.
Reduced directory request size alone does not establish an end-to-end speedup.
Reader authorization, process/input resume, sustained compaction under broader
loads, platform/recovery coverage and full performance parity remain open.

## Fresh comparison with the mutable baseline

[All four reports and ordered samples](sustained-write-comparison.json) compare
current driver `32c9d2f` with mutable upstream `f4d5b78`, in the order current,
baseline, baseline, current. Each process resets the same owned fixture, uses
the same harness hash, and performs 64 alternating updates after one warmup per
table. There were no observer probes, concurrent builds or tests during these
runs. Native library hashes and actual observed storage models are retained;
current reports protocol 15, baseline mutable storage. The baseline library was
built from its archived source using the same versioned Make target/toolchain.
All four runs passed every RETURNING and complete final-row check with no caller
retries. The final run leaves the fixture using current snapshot storage.

| Driver | Median update ms | p95 ms | Maximum ms | Updates/second |
|---|---:|---:|---:|---:|
| Mutable f4d5b78 | 4.975 | 7.553 | 10.808 | 190.879 |
| Snapshot 32c9d2f | 1709.874 | 6589.798 | 6818.636 | 0.435 |

Each row pools 128 serial update samples from two independent processes. p95 is
nearest-rank; throughput is total timed operations divided by total interval
seconds. Complete final verification is outside those intervals. These are
observed local results, without a statistical generalization across deployments.
The snapshot median is 343.7 times the mutable median; mutable throughput is
438.4 times snapshot throughput on this workload. Performance parity is not
achieved. The directory-page reduction does not close the gap, and the earlier
probed diagnostic is not a controlled before/after speedup comparison.

The next profiling target is the cost of ordinary point-write discovery,
publication and repeated compaction. Compaction currently emits one fenced Lua
command per output row; that is a concrete candidate, not an established sole
cause of the measured gap. Preserve exact visibility and durable outcome/lease
fences while investigating; do not hide stalls by increasing batch/segment limits,
adding SQL retries or reducing the dataset.
