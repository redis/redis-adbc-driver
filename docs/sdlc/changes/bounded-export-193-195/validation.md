# Validation and open gates

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
- The new public Go ADBC harness independently passed tiny-fixture checks for
  regular/partition APIs with one/four readers. Three Python checks passed for
  duplicate/type-sensitive validation, identifier quoting and binary-transparent
  request/reply counting. The full benchmark additionally records complete-fetch
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

## Performance and feasibility gaps

Existing S3 inventory includes the historical full-scale Software-origin backup
(40 files, 50,181,080,429 compressed bytes, 99,997,497 rows in its manifest) and
an independent source Parquet object for the historical full backup. These are
discovery results, not a fresh restore/compatibility validation. The previous
benchmark deployment is terminated. Do not treat arbitrary running AWS instances
as owned test resources. Backup checksum/metadata compatibility, QPF and typed
source identity must be verified again before timing.

Albert approved a $300 AWS cap and eight-hour hard deadline. A two-instance
task-owned compatibility pilot and verified automatic cleanup are running;
the independent 20-row/105-field pilot check passed and the 49-instance full
fleet is restoring the complete compatible snapshot. Cleanup was reverified
against all 49 exact owned instances. Do not expose the private operational
state in the public evidence. One initial snapshot
restored rows but lacked catalog metadata (which belongs to a different shard);
the pilot was repeated with the checksum-verified catalog-owning snapshot.
The backup records eight loading workers, so its first internal IDs cannot be
assumed to equal the Parquet prefix. The independent reference must match native
HASH membership to unique official-source WatchIDs and validate all 105 fields.
The isolated local Docker fixture was removed after the integration checks;
unrelated containers were left alone.

The 100K × 105/narrow one/four-reader complete-fetch matrix has **not run** yet.
No elapsed, CPU, allocation or network improvement is claimed, and normal builds
do not enable prefetch. Issue #193 is not ready to close on these checks alone.

The typed-export document records a contract and the requested engineering
no-go disposition. The dedicated read-only probe reproduced loss of exact
BIGINT semantics under naive Lua numeric conversion; its sanitized result is
retained separately. No named server-owner approval or supported new native
server API was available. No production API/storage change is selected; a future
integration must obtain that external approval and satisfy the conditional
feasible-prototype performance gate. This is a no-go investigation result, not
a mock codec promoted as a native server prototype or an approval.

## Review status

No human review/approval is claimed. Self-review checks stage ownership,
serialized cursor cleanup, cancellation precedence, fallback eligibility,
range transitions and retained Arrow ownership. Outstanding performance and
server feasibility gates are explicit. Do not close either issue or mark the
overall goal complete until its required evidence is obtained.
