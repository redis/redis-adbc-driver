# Validation and review

Date: 2026-11-12. Base: `349f1e32`; candidate: `codex/managed-performance`.
See [intent](intent.md), [contract](spec.md), [plan](plan.md), and
[sanitized measurements](measurements.json). No release or maintainer approval claimed.

## Environment and method

Every live invocation used `go/software-setup.sh --exec`, verifying Redis
Software RAM/noeviction, three active primary shards, certificate-verified TLS,
RESP3 and effective active 4x QPF (six Search workers, mnp routing, 32 connections)
before execution. Redis reported 8.8.0. Hardware/container limits are in the JSON.
No Community or unsupported-server tests were run. Credentials, endpoints,
licenses and operational logs remain private.

`TestManagedSingleGenerationDirectoryScan` failed on the base because an
ordinary committed single-generation scan opened a Search data cursor. It now
uses the existing immutable directory reader, preserves exact BIGINT/NULL values,
leaves planning/persisted metadata unchanged, and retains the original values
under an old pin after another statement publishes an UPDATE.
`TestManagedDirectoryPagesRetainTail` consumes 4,097 exact rows through 17-row
Arrow batches, checking order, values beyond 2^53, NULLs, the final directory-page
tail and complete reader consumption. No test uses a latency threshold.

## Checks

Commands run from `go/` unless otherwise stated:

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`: passed;
  final root-package time 27.387 seconds. Endpoint-dependent tests skip, so this
  pass establishes unit/package checks, not live compatibility.
- `bash software-setup.sh --exec sh -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI";
  go test -race -count=1 -v -p 1 -timeout=15m
  -run "TestManagedSingle|TestSnapshot|TestSQL(Wide|Scan)" .'`:
  passed in 442.595 seconds, 630 pass events and 75 skip events including subtests.
  This broad pass preceded the final larger single-directory page addition.
- Final-code targeted race run:
  `bash software-setup.sh --exec sh -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI";
  go test -race -count=1 -v -p 1 -timeout=10m
  -run "TestManaged(SingleGenerationDirectoryScan|DirectoryPagesRetainTail)$|TestSnapshot(DirectoryMerge|Delta|Native|Read|DirectPartition|FinitePartition|IndexProof)|TestSQLScan" .'`:
  passed in 9.810 seconds, 66 pass events and seven skip events.
- `bash software-setup.sh --exec sh -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI";
  go test -race -count=1 -v -p 1 -timeout=5m
  -run "TestNativeReader|TestSQLScan(Projection|ArrowEarlyRelease)BothAPIs" .'`:
  passed in 4.025 seconds, 12 pass events, no skips.
- `make -C go build VERSION=v0.0.26-dev.managed-perf`: shared-library build passed.
- `git diff --check`: passed. Byte-budget equivalence and existing canonical-ID,
  newest-version/tombstone, cancellation and partition-bound unit tests passed.

The 75 broad-run skips include 52 Cluster-named events requiring a separate
verified Software Cluster API endpoint and, for recovery cases, an owned
restart/failover/RDB orchestrator. Other skips require temporary restricted ACL
principals (including DEL/ZADD/HDEL/ZREM/index-deletion denials, EVAL-only and
no-HGETALL operators), the reader lease-model API gate, preserved protocol-15
worker libraries with Python, or explicit profiling flags. The final targeted
run skipped the read-resource profile and six reader lease-model cases. These
are coverage gaps, not evidence that those configurations passed.

## Before/after observations

The same opt-in `TestManagedPerformanceProfile` was run in a temporary detached
base checkout and the candidate, sequentially, on generated 4,096-row x 64-column
BIGINT data. Every complete result and duplicate multiplicity was checked outside
timing; UPDATE/DELETE overlay results were independently checked too. Read timing
includes complete Arrow consumption and test-harness string rendering with an
already-open connection. One warmup plus three measured trials per shape:

| Read shape | Base median | Candidate median | Ratio |
|---|---:|---:|---:|
| All 64 columns | 272.785 ms | 106.988 ms | 2.55x |
| One column | 171.047 ms | 41.876 ms | 4.08x |
| All columns, 101-row indexed range | 167.708 ms | 52.818 ms | 3.18x |

Representative read command counts show full scans replacing Search data
cursors and repeated range-readiness probes with exact directory reads; the
initial published-segment FT.INFO/directory validation pipeline remains.
Single-directory pages match the existing 4,096-row positional HASH pipeline
ceiling. Multi-segment and compaction page bounds remain unchanged.

The one timed 4,096-row SQL INSERT SELECT per revision observed 508.045 ms versus
487.835 ms. A single write sample does not establish an ingestion throughput
speedup. Fenced row writes and final index notifications remain the dominant
write cost; this change deliberately retains their serialization, durable
outcome and readiness/publication proofs.

Client microbenchmarks (three samples, separate from SQL wall time):

- 64-column row byte accounting: median 2,505 ns -> 43.93 ns, 128 allocations /
  1,217 bytes -> zero. The budget is identical, including non-string fallback.
- Directory merge of 256 visible rows: median 26,820 -> 16,540 ns (one segment),
  36,249 -> 23,127 ns (four), 45,340 -> 29,447 ns (eight). Allocation counts fall
  from 940/1,196/1,196 to 526; output identities and newest winners are checked.

Reproduce with `go test -run '^$' -bench
'BenchmarkSnapshot(DirectoryMerge|ImageByteBudget)' -benchmem -count=3 .`, and
run the live profile through the Software setup gate with
`REDIS_SNAPSHOT_URI="$REDIS_URI" REDIS_MANAGED_PERF=true` and
`go test -count=1 -v -p 1 -run '^TestManagedPerformanceProfile$' .`.
These shared-workstation observations are not confidence intervals or a published
100K/105-column table-mode, 100M ClickBench, physical Flex, or platform-matrix rerun.

## Review

Self-review against `REVIEW.md`: checked descriptor-copy isolation, source and
SQL ACL boundaries, exact identity validation, residual predicates and NaN/type
fallbacks, old-pinned reads, bounded pages/tails, Arrow release/cancellation,
compaction checkpoints and unchanged byte quotas. Initial index validation and
reader-pin lifecycle remain required. The write outcome slot, writer fences,
final indexing and publication protocol are unchanged. This is not human approval.
