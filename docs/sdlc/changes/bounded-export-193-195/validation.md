# Validation and dispositions

2026-11-03, baseline `6d7ac40`; macOS arm64, Go 1.27.2.
See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

## Environment and observed checks

Used an isolated local Redis Software 8.6.2 database with Search, three shards,
authenticated verified TLS and active 4x QPF. `make -C go redis-up` verified
scaling factor 4, six Search workers, mnp routing and 32 connections through the
management API. Fixed test credentials and public CA remain under ignored
`go/build`; no operational configuration is committed.

- Experimental unit suite: `env -u REDIS_URI go test -count=1 -p 1 -tags
  scanprefetch ./...` passed all five packages. Integration tests skip without
  `REDIS_URI`; that pass alone does not establish server compatibility.
- Focused experimental race integration passed: `TestPrefetchUpdateDeleteAfterDiscovery`
  and `TestSQLWideHashFetchBothAPIs`. The latter covers regular/public partition
  APIs, exact values, aliases/duplicate outputs, all-NULL rows, missing defaults,
  concurrent HASH updates/deletes, fallback predicates/order/LIMIT, cancellation,
  release and retained Arrow integer/string buffer lifetime.
- Broader experimental race integration passed the streaming, wide projection,
  shared scan and transport selection after the cancellation-precedence fix
  (28.942 seconds, no race reports).
- Default-build unit suite passed all five packages, and the experimental cgo
  shared library built with the repository's version linker flag.
  After the transport join barrier, the default suite passed again (26.543 s
  in the driver package), and the focused Software race suite for wide reads,
  streaming, mutation, prefetch and transport passed (20.054 s).
  The final experimental full unit/race suite also passed all five packages
  (48.649 s in the driver package). Unexpected producer panics retain ADBC
  `INTERNAL` status, verified by the terminal-error test.
- The new public Go ADBC harness independently passed tiny-fixture checks for
  regular/partition APIs with one/four readers. Six Python checks passed for
  duplicate/type-sensitive validation, identifier quoting and binary-transparent
  request/reply counting, changed source identity and missing/duplicate native
  reference membership. The tracked reference builder obtains every expected
  value from the checksum-verified official source. The full benchmark records complete-fetch
  Go allocations, client CPU, sampled heap and lifetime peak RSS; these are Go
  Arrow allocator results rather than the C shared-library allocator.
- Prefetch unit/race tests passed: one-page bound and order, reply ownership,
  cancellation at fetch/delivery/conversion, terminal fetch errors/EOF and worker
  panic propagation. Cancellation versus a closed queue has an explicit regression
  test so it cannot silently return successful EOF.
  A deterministic consumer-callback cancellation test also passes under the race
  detector and asserts that cleanup joins the outstanding producer.
  Actual blocked HMGET/cursor READ socket tests pass without command replay.
  A close observer verifies that pool shutdown waits for worker completion and
  prevents starting another worker after the transport begins finishing.
- Synthetic retained-heap diagnostic (race build): one owned 4,096 × 105 string
  page used about 28.42 MB; one additional lookahead used about 28.44 MB.
  Unique roughly 32-byte strings plus Go object/slice overhead are included.
  GC `HeapAlloc` deltas are neither RSS nor cumulative allocations; this is not
  a ClickBench fixture or a performance-gate result. Each reader has its own
  lookahead budget; four readers can retain four extra pages. Real pages and
  variable-width values determine the actual bound.

Initial attempts are retained as failures, not hidden by skips: the sandbox
blocked loopback listeners in the TLS/proxy unit tests; rerunning with network
access passed. The shared default Software container was stopped externally
after setup, causing connection refusals; the isolated fixture passed instead.
No assertions or supported-server tests were weakened.

## Completed restored-fixture screen

The [full report](../../../../benchmarks/prefetch/2026-11-03/README.md) retains
all eight cases, elapsed ranges, actual first batches, client CPU, cumulative
allocations, sampled heap/lifetime RSS interpretation and separate request/reply
wire diagnostics. It links 96 primary trial records and 32 separate wire records;
every execution passed independent complete-value, physical-type, NULL-bit and
duplicate-multiplicity validation. Every requested reader was active. Each trial
verified Redis Software 8.6.2, 48 shards, TLS/RESP3 and active 4x QPF.

All 48 snapshot files passed actual SHA-256 verification. The full compatible
99,997,497-row backup was restored on Software; historical Community snapshot
provenance is not relabeled as Software. No Community benchmark was run. The
100K fixture was copied from exact restored internal IDs using the pinned DDL
and SQL INSERT SELECT outside timing; no Parquet ingestion or full-scale reload.
Native ID/unique WatchID membership selects every expected value independently
from the checksum-verified official Parquet. See the report's source proof.

Measured driver revisions: baseline `6d7ac40`, experimental `87a48c5`, Go 1.27.2
Linux amd64. Wide one-reader medians improved 13.50% regular / 15.41% partition,
winning all five pairs with nonoverlapping ranges. Four-reader ranges overlap;
the narrow regular four-reader median regressed 1.77%. Keep the experimental
build tag **disabled by default**. No native-export or Python/C-boundary speedup
is established and no universal production performance claim follows.

After the benchmark, final shutdown review reproduced a connection Close race
during initial ExecuteQuery fill: the transport existed before its public reader,
so the maintenance pool could close before worker cursor cleanup. The regression
test failed before the fix. Connection Close now joins transports; the worker
finishes serialized cursor cleanup before announcing completion. The final full
experimental unit/race suite passed all five packages (driver 48.727 s); default
unit suite passed all five (driver 27.087 s). The experimental cgo shared library
built successfully. These shutdown follow-ups have not been rebenchmarked;
performance numbers stay pinned to `87a48c5`, not final PR HEAD. Worker finalization
can intersect terminal stream cleanup, so unchanged final-HEAD elapsed is not
assumed. This does not promote the prototype.

Setup failures are retained: the first pilot selected a snapshot without catalog
metadata; the catalog-owning shard passed a fresh independent 20-row × 105-column
check before expansion. A full-table row-ID predicate correctly failed the
public partition eligibility guard, requiring the separate small fixture. The
first baseline warm-up exposed source DDL NOT NULL versus existing SELECT
nullable field metadata. Diagnosis found no name/type/value/validity differences;
validation now models output field nullability without changing any NULL bit or
value. A regression test rejects actual NULL changes. Failed raw outputs remain
in the private archive. A final local test attempt used the AWS-only Python
environment without PyArrow; rerunning in the existing Arrow environment passed
all six Python tests. No assertion was weakened or failed output admitted.

The isolated local Software fixture was removed after integration tests;
unrelated containers were left alone. Albert approved a $300 total AWS cap and
eight-hour deadline. All 49 task-owned instances are terminated. No active/attached disks or network interfaces remain; five disks still report
AWS asynchronous `deleting` state after deletion was accepted. Do not claim zero
physical volumes yet. The zero-volume wait timed out; final sanitized verification
distinguishes this from active resources. Task security group, key and cleanup
automation are deleted; existing backups and shared IAM resources are preserved.
See the report's cleanup record. Conservative compute upper estimate: $46.01,
excluding storage/network and not a final invoice. Cleanup actions completed
within the eight-hour deadline.

Current main added CUDA execution and Go 1.27.3 while the screen ran. The branch
integrates that work and explicitly excludes GPU scans from prefetch eligibility,
preserving its existing path. This integration also postdates the pinned screen. On Go 1.27.3, the combined
branch passed the complete default suite (driver 26.793 s) and experimental race
suite (driver 52.015 s), including the additional CUDA stub package. The merged experimental cgo shared
library also built successfully.

## Typed export disposition

The [typed export document](../../../typed-export-design.md) completes the
versioned logical contract, four-way alternatives comparison and the requested
engineering **no-go for production integration**. The dedicated read-only
Software probe reproduced exact BIGINT loss under naive Lua numeric conversion;
its sanitized result is retained. This is a limited diagnostic, not an exhaustive
proof that no server implementation is possible. No named server-owner approval
or supported new native export API was available. No production API/storage
change is selected. Future integration requires that external decision and the
conditional feasible-native-prototype elapsed gate. Closing the design/no-go
investigation does not claim a native prototype shipped or owner approval.

## Self-review

Reviewed stage ownership, cursor serialization, readiness/existence/fallbacks,
reply/Arrow lifetime, range transitions, cancellation precedence, blocked sockets,
initial-fill connection Close, transport registration/join races and diagnostics
for private data. No unresolved reproduced correctness defect remains. RESP3 is
the current repository contract; no RESP2 support or standalone/Cluster promotion
claim is added. No ACL/TLS/routing/storage dependency changes are introduced.
The two implemented dispositions are a bounded experimental client prototype and
a typed-server-export no-go recommendation. Maintainer human review is still
required before merge; no human reviewer or approval is claimed here.
