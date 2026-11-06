# Point-write index readiness profile

Date: 2026-11-04. Parent: 12851f4 (production Go tree equals 32c9d2f).
Status: diagnostic and private-index prototype; production path unchanged.
PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[the failed sustained-write parity gate](sustained-write-validation.md).

## Attribution

A private native diagnostic used two 20,000-row tables. Two indexed `id=1`
updates and two direct `__rowid=1` updates each processed one matching row,
returned the independently expected exact BIGINT increment and retained all
40,000 final rows/values/payloads. Wall times were 1.435–1.470 seconds; execute
samplers recorded 0.152–0.158 summed Redis round-trip worker seconds. The
remaining elapsed time is not measured server CPU. Both discovery forms have
similar latency, providing no evidence that a full target-row scan is the main
cost here. [Sanitized diagnostic data](point-write-profile.json) retains all
four observations and the runner/library hashes. This instrumented private
runner overrides the comparative harness's workload; it is not a normal
benchmark report or an isolated latency comparison.

The reproducible Go profile traces newly created indexes' FT.INFO replies,
counting unsettled initial scans and measuring create-to-first-ready elapsed
with the expected document count and zero failures. It observes commands without
rewriting them. A fixed one-row UPDATE is profiled first with a 20,000-row target,
then with an additional unrelated 20,000-row table. Setup is outside the measured
update. The profile has no latency assertion; correctness and complete
observation are required. Latest results are recorded below.

Redis documents that [FT.CREATE scans existing keys unless SKIPINITIALSCAN is
specified](https://redis.io/docs/latest/commands/ft.create/). The observed
readiness delay grows sharply after unrelated rows are added. This makes final
index construction the immediate target; reducing compaction's directory page
alone cannot remove ordinary point-write readiness waits.

## Aborted private-index experiment

The second opt-in test stages one replacement row over the same populated
target/unrelated fixture. It creates an empty private index with SKIPINITIALSCAN,
then issues a lease/ownership-fenced HSET of the unchanged row-ID field. Search
receives the already-final row image after its index exists. The test waits for
indexing complete, full initial progress, zero indexing failures, expected
physical prefix and one indexed document. A query over the ID and changed
BIGINT indexed fields must find exactly one result. Full private HASH maps must
remain identical, and public SQL must still read the old committed value.
The workspace is always aborted; the experiment never calls publication.

An initial run completed create/notification/readiness in 2.231 ms, and a follow-up
in 1.864 ms. These instrumented, single-row prototype observations are not a
production update speedup or sustained-compaction result. Latest results follow.

A production strategy still needs bounded directory traversal, exact member/row
identity and ownership checks, no image changes, cancellation/unknown-outcome
handling, completion proof and unchanged publication/index-prefix fences. It must
cover empty and multi-row outputs, tombstones, NULL/native encodings and all
supported index schemas, plus concurrent writers, recovery and platforms.
Do not enable a shortcut based only on this one-row experiment or bypass the
readiness checks to make timing pass. The full performance gate remains failed.

## Reproduction and scope

Use the owned disposable Software fixture and run sequentially:

```
export REDIS_SNAPSHOT_URI="$REDIS_URI"
export REDIS_SNAPSHOT_WRITE_PROFILE=1
make -C go test-software TEST_ARGS='-race -json -failfast -run TestSnapshotPointWrite'
```

The Make setup verifies active/effective Software 4x QPF and noeviction before
testing. These cases clear the disposable snapshot database. The two test cases
skip unless the profiling environment variable is set; this is test diagnostics,
not an opt-in product feature. Actual runs used strict TLS, Redis 8.8.0, three
primary shards behind the Software proxy, macOS ARM64 and Go 1.27.3. Native
versions were Python 3.13.16, PyArrow 26.0.0 and ADBC manager 1.13.0. There is no
Cluster API/failover/AOF/platform-parity claim or production protocol change.
Private logs retain full execution output; operational identifiers stay private.

## Final recorded run

The final observer uses identity dial/pipeline hooks and records first readiness
only when the one-document count and zero-failure checks also hold. Both selected
race tests passed, zero test skips/failures; root package 12.268 seconds.
The CUDA support package had no applicable tests. `git diff --check` passed.

| Unrelated rows | Update wall ms | New-index ready ms | FT.INFO polls | Unsettled polls | Poll round-trip ms |
|---:|---:|---:|---:|---:|---:|
| 0 | 780.125 | 746.081 | 56 | 55 | 84.292 |
| 20,000 | 1430.743 | 1401.825 | 104 | 103 | 167.818 |

The final aborted one-row directory-index experiment recorded 2.330 ms.
Repeated diagnostic runs are all retained in private logs; no successful run
activates this alternative in production. An early observer could not unwrap
the no-retry command's result and reported zero readiness delay; it was corrected
and is excluded from attribution. There is no new native throughput measurement
or full-suite/platform result for a production indexing change, since no such
change is included here.
