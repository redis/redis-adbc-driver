# Sustained-write diagnostics and bounded compaction reads

Date: 2026-11-04. Parent: 9fbbaa4. Status: focused regression matrix passed; matched timing pending.
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
with their observer-probe qualification. Fresh matched-source timings remain
pending.
Reduced directory request size alone does not establish an end-to-end speedup.
Reader authorization, process/input resume, sustained compaction under broader
loads, platform/recovery coverage and full performance parity remain open.
