# Validation and review

Date: 2026-11-12. Base: `8ed19932`; candidate: working tree on
`codex/unmanaged-performance`. See [intent](intent.md), [contract](spec.md),
[plan](plan.md), and [sanitized measurements](measurements.json).

## Reproduction and correctness

`make -C go redis-up` created/reused the disposable Redis Software fixture.
Every integration/diagnostic invocation used `go/software-setup.sh --exec`,
which verified active 4x QPF, six Search workers, mnp routing, 32 connections,
RAM/noeviction, TLS and three active primary shards before execution. Redis
reported 8.8.0. No Community, Valkey, ElastiCache or Memorystore tests were run.
Credentials, endpoints, license/certificate files and operational logs stay private.

The new focused tests failed before implementation:

- 300 INSERTs issued 300 serial mutations and zero pipelines.
- An indexed native integer range planned query `*`, scanning the full source.

They pass after implementation: 300 mutations use three bounded pipelines;
a range matching two of 101 rows fetches/decodes exactly two candidates.
Regression coverage includes byte-bounded pipelines; duplicate/existing keys;
a source key created after preflight; counting successes after a failed row;
unknown pipeline replies with no subsequent batch/input replay; cancellation;
source encodings; integer precision beyond 2^53; index replacement/fallback;
live HASH changes/deletion/type changes between discovery and fetch; foreign
FILTER/multiple-prefix membership; and application data/index ownership.

Commands executed from `go/` unless noted:

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  passed; integration tests skip without endpoints, so this is unit evidence.
- `bash software-setup.sh --exec sh -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI";
  go test -race -count=1 -v -p 1 -timeout=10m -run "Test(Unmanaged|FlexUnmanaged)" .'`:
  passed (11.836 seconds). Only the opt-in performance sample skipped.
  Flex stopgap behavior was exercised on the Software RAM fixture; no physical
  Flex deployment or Flex performance is claimed.
- `bash software-setup.sh --exec go test -race -count=1 -p 1 -timeout=10m
  -run 'Test(SQLWideHashFetchBothAPIs|ScanProjection|SQLAdopt|SQLForeign)' ./...`:
  passed; root package 37.163 seconds, other packages had no matching tests.
- Final all-package unit pass: passed; root package 27.382 seconds.
- Final unmanaged plus SQL projection/early-release race checks:
  `bash software-setup.sh --exec sh -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI";
  go test -race -count=1 -v -p 1 -timeout=10m
  -run "Test(Unmanaged|FlexUnmanaged)|TestSQLScan(Projection|ArrowEarlyRelease)BothAPIs" .'`:
  passed (10.898 seconds), 112 pass events including subtests, no failures;
  only the opt-in performance sample skipped.
- `make -C go build VERSION=v0.0.26-dev.unmanaged-perf`: passed, shared library built.
- `git diff --check`: passed.

## Small before/after diagnostic

`TestUnmanagedPerformanceSample` is opt-in with `REDIS_UNMANAGED_PERF=true` and
`REDIS_SNAPSHOT_URI` pointing at the verified disposable Software fixture. The
same sample test was copied into a temporary detached checkout of the base.
The generated input has 4,096 rows and 64 BIGINT columns. Every stored field,
complete query value and duplicate multiplicity is checked outside timing.

| Operation | Base | Candidate |
|---|---:|---:|
| One `store.insertRows` call, one sample each | 3.369498 s | 0.122120 s |
| One reader, median of three complete scans | 0.180050 s | 0.171808 s |
| Four readers, median of three eight-range workloads | 0.424751 s | 0.094550 s |

Writes include key preflight and mutation, excluding conversion/input setup.
Read timing includes Arrow conversion and test-harness string rendering, using
pre-opened connections. Hardware and container limits are recorded in the JSON.
No warmup; shared workstation. These observations are
approximately 27.6x lower write time and 4.5x lower four-reader range time in this
small diagnostic, not end-to-end SQL/Arrow ingestion or production estimates.
The initial all-scan key/HASH-fetch experiment slowed full scans; the final
implementation keeps full scans on LOAD * and fetches HASHes after filtered
unordered discovery. Other experiments are not relabeled as final evidence.

The published 100K/105-column table-mode benchmark, physical Flex deployment,
100M ClickBench and the wider platform/Cluster API matrix were not rerun.
Historical evidence remains unchanged. No release or merge is claimed.

## Review

Self-review against `REVIEW.md`: inspected complete diff, source ownership,
single-key slot routing, NoRetry markers, all-reply acknowledgment accounting,
stop-before-next-batch behavior, cancellation, mutable index-value shortcuts,
source definition refresh and exact HASH residual predicates. The pipeline tail
can apply after an error; documentation explicitly describes non-prefix counts.
This is not human maintainer approval.
