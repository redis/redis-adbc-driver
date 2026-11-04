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
- Prefetch unit/race tests passed: one-page bound and order, reply ownership,
  cancellation at fetch/delivery/conversion, terminal fetch errors/EOF and worker
  panic propagation. Cancellation versus a closed queue has an explicit regression
  test so it cannot silently return successful EOF.
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

The 100K × 105/narrow one/four-reader complete-fetch matrix has **not run**.
No elapsed, CPU, allocation or network improvement is claimed, and normal builds
do not enable prefetch. Issue #193 is not ready to close on these checks alone.

The typed-export document is a contract and engineering recommendation. No
named server-owner feasibility/contract decision or supported new server API
was available. Issue #195's external feasibility acceptance remains open. No
mock codec result is represented as a server prototype or approval.

## Review status

No human review/approval is claimed. Self-review checks stage ownership,
serialized cursor cleanup, cancellation precedence, fallback eligibility,
range transitions and retained Arrow ownership. Outstanding performance and
server feasibility gates are explicit. Do not close either issue or mark the
overall goal complete until its required evidence is obtained.
