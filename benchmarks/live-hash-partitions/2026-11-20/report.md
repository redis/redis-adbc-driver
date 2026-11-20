# Live HASH partition comparison

Date: 2026-11-20. Source: `f28dbc042bbcd4e28b243c6e8e3d33fe893d0782`.
This is a small synthetic source-read comparison for
[issue #550](https://github.com/redis/redis-adbc-driver/issues/550), not ClickBench
or packaged-release/performance parity evidence.

## Environment and method

Redis Software, Redis 8.8.0, one primary shard, RAM, TLS/RESP3 and noeviction.
Setup verified active 4x QPF, six effective Search workers, mnp routing and 32
proxy connections. One shard was selected after the initial three-shard fixture
could not allocate resources; the readiness/QPF gates were preserved.
Client: macOS 26.7.3, arm64, Go 1.27.4, Docker published TLS-port route. This was a
shared development host, not isolated hardware. Other containers and builds can
affect wall time; server counters include only the owned Software instance.
Operational identities, credentials, certificates and raw logs remain private.

The fixed source contains 8,192 HASHes with BIGINT `n` and VARCHAR `v`; source keys
are projected as BYTEA. Compare one bucket/reader with four concurrent buckets
and independently opened connections. Each reader scans the entire prefix and
keeps its SHA256 key bucket. Three trials alternate order (1/4, 4/1, 1/4).
Registration, index readiness and connection creation precede timing. The timed
interval includes descriptor read admission, complete Arrow fetch and collection
of result strings by the harness. Sorting and exact whole-result comparison run
after timing; every condition matched the independent key/value oracle.

Client CPU is process CPU consumed during the timed interval. Allocations include
the benchmark collector, not just driver buffers. Heap-after and server RSS are
point samples, not peaks. Lifetime peak client RSS includes earlier setup/trials.
Server CPU covers Redis/proxy processes and has coarse counter precision at this
scale. QPF is verified configuration, not a claim that Search workers accelerate
source SCAN/HASH retrieval or client Arrow encoding.

## Median of three trials

| Readers/buckets | Complete read (s) | Client CPU (s) | Server CPU (s) | Client allocated (MiB) | Client heap after (MiB) | Server RSS after (MiB) |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 0.252 | 0.077 | 0.040 | 23.06 | 18.32 | 194.69 |
| 4 | 0.307 | 0.071 | 0.060 | 26.48 | 13.29 | 194.69 |

The four-reader median was slower in this run, and individual trial ordering
varied. Repeated discovery increases total SCAN work; these results establish
independently schedulable exact fixed-source reads, not a general throughput
improvement or recommended worker count. No large-source, remote-network,
Cluster API, cold-storage or sustained-load claim follows.

## Reproduce

Use a dedicated disposable Software database and verify active 4x QPF through
`make -C go redis-up`; source its private generated environment. Set
`REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed` only after policy verification.
Run `go test -count=1 -p 1 -v -run '^TestLiveHashPartitionProfile$' -timeout=8m .`
from `go/`, with `REDIS_LIVE_PARTITION_PROFILE=true` and
`REDIS_FLEX_BENCH_CONTAINER` naming that owned Software fixture solely for process
counters. That variable's legacy name does not change this RAM-only source mode.
Do not publish its value. The fixture and registration are removed after checks.

[Sanitized measurements](results.jsonl) and [source/configuration metadata](metadata.json)
retain exact local provenance. Binary/NUL source coverage, unknown-only HASHes,
evolution/lease failures and cancellation are separately labeled correctness
tests; they do not relabel this fixed synthetic source.
