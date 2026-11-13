# Isolated allocation evidence — November 13, 2026

All **84 samples** passed result validation: 28 cases, three observations each.
The [complete results](results.json) include medians, every observation, pinned
module sums, source/fixture hashes and successful command status. Measured code
is commit `434905b945f27b53c8db90ea82804b7cd9ad3b4a`, based on merged main
`0359c0d6b602559ff80e461a6749854ca21f1f53`. Go 1.27.3, Apple M5 Max,
darwin/arm64, GOMAXPROCS=1, 200ms minimum per case, exclusive measurement slot.
No Redis server, transport, TLS, ADBC query, Python or IPC was measured.

## RESP3 decoding

Median per 4096-row page, 105 fields, 32 KiB reader buffer:

| Consumer | ms/page | Go bytes/page | Go allocations/page |
| --- | ---: | ---: | ---: |
| Actual SliceCmd decoder | 16.259 | 22,323,131 | 782,468 |
| Reused typed string/null slots | 11.933 | 9,005,656 | 404,782 |
| Fixed byte-slot prototype | 7.801 | 0 | 0 |

This isolates parser/materialization costs. Existing `ReadStringInto` permits
an allocation-free timed lower bound with fixed slots, but changes value lifetime
and reserves a 27.1 MiB value slab before timing. It excludes command construction,
hooks and the owned generic result contract; it is not a production API or RSS
claim. The typed string case still allocates owned payload strings. Reader buffer
changes to 1 MiB produced small, mixed timing differences; these in-memory tests
provide no evidence of a transport gain. All 256/1024/4096-row results are retained.

## Arrow boundary

Median per 10,000 total rows, 105 columns. Bytes and allocations below are **Go
only**; C allocations and prebuilt shared payload buffers are excluded.

| Handoff | 1K batches: ms / Go bytes / allocs | 10K batch: ms / Go bytes / allocs |
| --- | ---: | ---: |
| C Data, schema each, fresh headers | 1.243 / 2,746,217 / 21,332 | 0.124 / 274,634 / 2,133 |
| C Data, schema each, reused headers | 1.251 / 2,746,311 / 21,332 | 0.123 / 274,624 / 2,133 |
| C Data, known schema, reused headers | 0.715 / 1,440,603 / 12,365 | 0.071 / 144,066 / 1,236 |
| C Stream, schema once | 0.777 / 1,570,123 / 13,263 | 0.129 / 275,037 / 2,143 |

Caller header reuse did not show a consistent timing or Go allocation benefit.
Schema-known and schema-once contracts reduce repeated metadata work, but differ
from schema-each ownership and negotiation. Ten times fewer boundary crossings
roughly reduced metadata costs tenfold here; payloads are shared zero-copy, so
this is not a tenfold driver-write improvement. Go retain-only controls were
26.49ns and 3.61ns respectively, with no Go allocation. This evidence does not
establish Arrow as an end-to-end bottleneck.

## Ownership and follow-up

Race and checked-allocator tests cover full/early stream release, records retained
after reader advance/release, moved C Data header reuse, all original source
references released, and independent concurrent lifetimes. A minimal counted
producer confirms ExportRecordReader retains a reference and stream release drops
one: the caller must release its original reference. A private overlay omitting
that release fails both reference counts and allocator balance as expected.

The pinned Arrow v18.8.0 comment saying caller Release is unnecessary disagrees
with that callback chain. See the [exact ownership account](../README.md#producer-reference-accounting).
The actual driver's custom exporter has a separate balanced reference path;
these findings do not demonstrate a driver leak.

Prioritize an explicit lifetime/hook contract before requesting an HMGET buffer
API; measure realistic command construction and transport separately. For Arrow,
clarify the ownership comment upstream, and compare schema-known/stream contracts
only where consumers can honor them. The [road-test assessment](../README.md#companion-transport-harness)
is a companion plan; its code was neither copied nor run for this evidence.
Raw logs, private copies and negative-control artifacts remain private. No
upstream messages or PRs were published by this work.
