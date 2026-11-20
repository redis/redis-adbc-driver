# Unmanaged deferred-read comparison

Date: 2026-11-20. Measured source: `222ab181`, based on `1d0d0b3b`.
See [source fingerprints](provenance.json), [individual results](results.jsonl),
and the [change record](../../docs/sdlc/changes/issue-549-unmanaged-partitions/intent.md).
These are local synthetic measurements, not ClickBench, release evidence or a
full-scale throughput claim. The later cached-layout test is not benchmark coverage.

## Environment and method

Dedicated Redis Software **8.2.6-58** RAM and actual Software Flex fixtures, Redis 8.8.0 with
Search, one primary shard each, TLS/RESP3 and noeviction. Both repository setup
paths verified active 4x QPF, 6 effective Search workers, mnp routing and 32 proxy
connections. RAM database size was 1 GiB. Flex used 1 GiB total with 256 MiB RAM and a
container directory for flash; the small dataset fits in RAM. This does not
measure cold SSD, Cloud or out-of-core behavior. Operational identities,
credentials, certificates, license files and raw logs remain private.

The client was macOS arm64 through a Docker published TLS port on a shared
developer host. Other local activity can affect scheduling; this is not an
isolated physical-host transport comparison. Published-port results must not be
generalized to colocated Linux clients.

Each fresh fixture contained 4,096 fixed application HASHes and an application
index, with exact BIGINT values above 2^53, text and an unmodeled field. A row-local
predicate selects 1,366 rows. Three alternating-order trials compare ordinary
ExecuteQuery, one unmanaged hash-bucket partition and four concurrent buckets.
Every result is fully consumed and checked for exact key bytes, values, count and
multiplicity against an independently generated oracle. Connections and fixture
setup are excluded; descriptor discovery is reported separately from read time.
No automatic worker tuner or background compactor is involved.

## Median observations

Rows below are medians of three complete trials. CPU values are process CPU,
not wall-time percentages. Allocations are cumulative Go allocations during the
read; RSS values are process snapshots/lifetime metrics, not isolated query peaks.

| Deployment/path | Read s | Discovery s | Client CPU s | Server CPU s | Client allocated MiB | Heap after MiB | Server RSS after MiB |
|---|---:|---:|---:|---:|---:|---:|---:|
| RAM: ordinary SELECT | 0.0337 | 0.0000 | 0.0213 | 0.0100 | 13.21 | 9.80 | 191.98 |
| RAM: one bucket reader | 0.8066 | 0.0239 | 0.1273 | 0.3600 | 17.53 | 9.21 | 189.29 |
| RAM: four bucket readers | 0.3694 | 0.0180 | 0.1584 | 0.4200 | 21.83 | 14.20 | 188.86 |
| FLEX: ordinary SELECT | 0.8865 | 0.0000 | 0.1314 | 0.4200 | 16.49 | 8.80 | 217.35 |
| FLEX: one bucket reader | 0.8365 | 0.0131 | 0.1428 | 0.4300 | 17.24 | 8.43 | 218.78 |
| FLEX: four bucket readers | 0.3551 | 0.0308 | 0.1655 | 0.4600 | 20.48 | 10.94 | 219.36 |

Four readers completed this small fixed workload faster than one hash-bucket
reader, while using more client CPU/allocation and repeating discovery. Ordinary
RAM SELECT was substantially faster than either partition path. This supports
an independently schedulable source path, not replacing ordinary SELECT for
speed. Each worker still scans the whole source: discovery work is O(P*N), and
SCAN MATCH may traverse unrelated database keys and repeats for every prefix.
Source key-state credits, server capacity, wider values, skew or different
transport routes can reverse the result. No universal speedup follows.

## Reproduce

Use separate disposable Software RAM/Flex fixtures and verify 4x QPF first with
`make -C go redis-up` and the Flex setup path. Source the private environment for
the intended fixture, then run only this opt-in profile against that fixture:

```sh
cd go
# Set REDIS_SNAPSHOT_URI to the selected verified fixture URI privately.
# Set REDIS_SNAPSHOT_FLEX=true only for the compatible experimental Flex path.
REDIS_UNMANAGED_PARTITION_PROFILE=true \
  go test -count=1 -p 1 -v -timeout=8m -run '^TestUnmanagedPartitionProfile$' .
```

The profile also requires `REDIS_FLEX_BENCH_CONTAINER` identifying the owned local
Software container privately, to read summed Redis/proxy process CPU and RSS.
Do not publish that identifier or source credentials. The profile intentionally
clears its selected fixture; never target an application database.
