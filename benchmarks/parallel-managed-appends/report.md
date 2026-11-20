# Parallel managed append measurements

Date: 2026-11-20. Request: [#547](https://github.com/redis/redis-adbc-driver/issues/547).
This is a synthetic Go ADBC comparison of four independent streams into one
managed table, with 4,096 rows per stream (16,384 total), three columns and three
trials per concurrency setting. It is **not ClickBench**, a full-scale load,
a Python/C-boundary benchmark, or cold-SSD evidence.

Each trial creates one empty table. The serial arm runs the same four appends
one after another; the parallel arm launches all four together. Trials alternate
arm order. Input construction is outside timing; ExecuteUpdate consumes/stages
input and acknowledges one atomic publication per stream. There are no caller
retries. Each call must acknowledge 4,096 rows. Complete SQL/Arrow oracles check
all IDs, distinct logical row IDs, exact BIGINT values above 2^53, text values,
sums, and final segment/directory/index cardinalities. Oracles run again after
maintenance. A wrong or partial result fails the measurement.

## Environment and provenance

Both endpoints are dedicated Redis Software fixtures with embedded Redis 8.8.0,
TLS/RESP3, noeviction and verified active **4x QPF**, six effective Search workers,
mnp routing and 32 proxy connections. RAM uses three primary shards and 1 GiB;
Flex uses one primary shard, BigStore v2, 1 GiB total and 256 MiB RAM, with flash
backed by a container directory. Compare arms within a deployment; differing
shard/storage configurations do not establish a direct RAM/Flex comparison.

The client is macOS 26.7.3 arm64, Go 1.27.4, using Docker-published TLS ports.
The machine and VM were shared with other development/testing work, including
other Software fixture startup. There is no dedicated-host isolation or
confidence interval. Native process CPU counters have coarse sampling precision.
The observed variation is a limit on any throughput conclusion.

RAM measured source `fb01d955`; Flex measured source `9f1749b5`. Between those
revisions, the append counter read gained typed timeout/cancellation error mapping
and profile formatting changed; successful append behavior is unchanged. Exact
source hashes and topology/settings are in [provenance.json](provenance.json).
[results.jsonl](results.jsonl) retains all twelve completed, sanitized samples.
The measured commit IDs precede an author-email privacy rewrite. Identical source
trees are available in public revisions RAM `08c64004` and Flex `2e350b2a`;
provenance records the full mapping, verified by tree identity. Source hashes and
measured samples were not relabeled. Operational identifiers, endpoints,
credentials, license files and raw private logs are excluded.

## Ingestion results

Values below are medians of three completed trials. Throughput is the median
per-trial rows/elapsed value. CPU seconds are cumulative process CPU, not wall
time or utilization percentages. Server CPU sums Redis/proxy processes in the
owned container; management processes are outside that sum.

| Deployment | Concurrent streams | Append seconds | Rows/s | Client CPU s | Server CPU s | Client allocations MiB | Server RSS after MiB |
|---|---:|---:|---:|---:|---:|---:|---:|
| RAM | 1 | 0.895 | 18,315 | 0.115 | 0.470 | 42.4 | 294.3 |
| RAM | 4 | 0.413 | 39,687 | 0.107 | 0.390 | 43.1 | 295.0 |
| Flex | 1 | 0.755 | 21,687 | 0.093 | 0.600 | 41.1 | 280.7 |
| Flex | 4 | 0.595 | 27,525 | 0.098 | 0.790 | 41.8 | 280.4 |

Allocations count bytes allocated during ingestion, not live or peak memory.
Server RSS is sampled before/after, not a phase peak. Raw samples additionally
record client heap after ingestion, client process-lifetime peak RSS, server RSS
before ingestion and server RSS after maintenance. The lifetime client peak can
include input construction and earlier test phases/trials; it must not be called
a per-append peak. No process-wide memory ceiling follows from these values.

## Admission, publication and maintenance

Admission is measured from each call's start to its successful native append
admission reply. Publication phase time spans the first CAS through the final
CAS, including intervening metadata rebase work; index preparation precedes that
interval. These are sums of per-stream wall intervals and can overlap, not server
CPU or a wall-time decomposition. Raw samples also retain only the publication
command durations separately.

Background compaction is disabled in both timed arms for attribution. After
acknowledgment and validation, the same durable maintenance worker is stepped to
completion with a two-minute drain budget. Its separately timed drain includes
protocol/cleanup overhead and is not foreground append time. Production-worker
concurrency is checked separately by the twelve-writer pressure test.

| Deployment | Concurrent streams | Sum admission s | Sum CAS/rebase phase s | Maintenance drain s | Maintenance client CPU s | Maintenance server CPU s |
|---|---:|---:|---:|---:|---:|---:|
| RAM | 1 | 0.126 | 0.005 | 3.599 | 0.504 | 1.910 |
| RAM | 4 | 0.091 | 0.092 | 5.543 | 0.559 | 2.490 |
| Flex | 1 | 0.074 | 0.002 | 6.143 | 0.440 | 2.320 |
| Flex | 4 | 0.082 | 0.203 | 6.112 | 0.441 | 2.160 |

Including the separately measured drain, median append-plus-maintenance time is RAM 4.550 s serial versus 5.955 s parallel; Flex 6.950 s serial versus 6.667 s parallel.
These tiny shared-host samples demonstrate functioning concurrent ingestion and
measure its overhead; they do not establish a general or full-scale speedup.
The metadata-slot row allocation/publication coordination remains shared.

## Reproduce and limitations

Use a dedicated disposable Software endpoint after `make -C go redis-up` verifies
its effective 4x QPF configuration. For Flex, provision and verify the dedicated
compatible Flex fixture as well. Select the explicit Flex driver option for
Flex. Keep the setup-generated endpoint/trust environment private.

From `go/`, with the correct private endpoint, trust and verified persistence
policy declaration already selected:

```sh
REDIS_SNAPSHOT_URI="$REDIS_URI" \
REDIS_PARALLEL_APPEND_PROFILE=true \
REDIS_FLEX_BENCH_CONTAINER="<owned-software-container>" \
go test -count=1 -p 1 -v -timeout=10m -run '^TestSnapshotParallelAppendProfile$' .
```

For actual Flex, use its private URI and `REDIS_SNAPSHOT_FLEX=true`. The harness
clears its database and Search definitions; use only the owned disposable fixture.
For cross-process, failure, lease, pin, conservative TAG/NaN, exact high-ID and
compaction-pressure coverage, see the separate
[verification record](../../docs/sdlc/changes/issue-547-parallel-appends/verification.md).
No packaged-release, failover/restart, large/cold-data or full-scale benchmark
claim is made. Repeat ClickBench work requires its compatible verified full-scale
backup and the repository's Software/Flex/QPF gates; this synthetic fixture is
not such a backup.

Earlier 65,536-row diagnostic attempts exceeded a 15-second test helper drain
budget. They are failed diagnostics, not samples in this report. Current results
use 16,384 rows and the explicit two-minute drain budget shown in the retained
profile fixture.
