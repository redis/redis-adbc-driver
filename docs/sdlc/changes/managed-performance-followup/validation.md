# Validation and review

Date: November 12, 2026. See [intent](intent.md), [contract](spec.md), [plan](plan.md).
Implementation source: `62b0f6671922f6d8e699c38957db70cac41eb6c6`.
Production base is `f630e117`; parent `30a1e5be` adds benchmark evidence only.

## Reproduction and design

The first opt-in 4K x 64 diagnostic attributed substantial time to row batches and
final index notifications. The larger 100K x 105 synthetic diagnostic separated
Arrow conversion, persistent-connection read execution and call-size effects.
The four-reader/eight-range workload issued 392 candidate searches and 448
pipelines. The regression `TestManagedCandidatePagesBoundedAndComplete` then
failed on base: eligible candidate page was 256 rather than the existing 4096-row
single-source HASH-fetch ceiling.

Eligible single-published-source candidate discovery and HASH fetch now use that
ceiling, capped by the server pagination limit. Multi-segment/private sources and
ineligible query shapes retain smaller pages. Keyset ordering, exact physical
identities, warnings/refusals, newest-version/tombstone validation and pin/index
proofs remain. The regression checks exact multiples, a one-row partial tail,
exact BIGINT values above 2^53, NULLs, complete fetch and command counts. Existing
prefetch tests request their prior 256-row merge bound explicitly after the
internal constructor gains a page-size parameter; their assertions are unchanged.

The CPU profile also identified integer ingestion coercion. Validated Int16,
Int32 and Int64 arrays with an exactly matching complete target type now bind
that fact per column. Narrowing, aliases, different target attributes, other
layouts and conversions retain the established coercion path. Input validation,
constraints, NOT NULL/defaults and ownership are unchanged. Comparative coercion
tests now include non-null int64 extrema and an integer alias.

## Live configuration and checks

Owned fixture started through `make -C go redis-up`, active verified 4x QPF/six
Search workers, mnp routing/32 connections, one RAM shard/noeviction, authenticated
certificate-verified data TLS/RESP3. Resized and verified at 4 GiB before larger
profiles; container four CPUs/eight GiB. Software 8.2.6-25, Redis 8.8.0/Search
8.8.8; Go 1.27.3; Apple M5 Max arm64, 18 logical CPUs/36 GiB. Profiles, tests and
native timing run separately on a shared workstation. Raw operational artifacts
and CPU profiles stay private.

- Focused `go test -count=1 -race -json -p 1` covering managed/candidate/index
  proofs, prefetch/visibility, ingestion and unmanaged Arrow: 261 pass events,
  eight conditional/profile skips, zero failures/race reports.
- Broader `go test -count=1 -race -json -p 1 -run
  '^(TestSnapshot|TestManaged|TestUnmanagedArrow|TestIngestColumn|TestArrow)'
  -timeout=20m .`, with the owned `REDIS_SNAPSHOT_URI`: 1050 pass events,
  78 conditional/opt-in skips, zero failures/race reports. Includes large
  compaction, rewrites, retained operations, import, candidate visibility and
  Arrow layout cases. Skips retain Cluster API, restricted-principal, profiling
  and other explicit environment prerequisites; they are coverage gaps.
- Unit-only all packages, `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test
  -count=1 -json -p 1 -timeout=10m ./...`: 1345 test pass events, 1255
  live/conditional skips, zero failures. No skips/assertions weakened.
- Selected ordinary Software SQL race checks run through
  `make -C go test-software`, with the owned project/ports and effective QPF policy
  verification: scans, wide HASH fetches, ordering, LIMIT, NULLs, adoption,
  foreign tables and ingestion. All 98 test events pass, no skips, failures or
  race reports; other packages have no matching tests. This is a selected
  ordinary suite, not a full default-suite rerun.
- Shared library/CLI build with `VERSION=v0.0.26-dev.managed-followup` passes.
  The source commit above pins native behavior independently of later evidence.

The diagnostic's first setup used Python ingestion mode strings with the Go API;
it was corrected to the ADBC Go constants before any timed trial completed.
That setup-error log remains private. No completed diagnostic samples were dropped.

## Performance evidence

The [report](../../../../benchmarks/table-modes/managed-followup-2026-11-12/report.md)
separates three forms of evidence. Every result is independently validated;
raw profiles/logs remain private and sanitized samples/counts are retained.

- The opt-in Go profile uses persistent connections and generated 100K x 105
  BIGINT/NULL data. Four-reader median: 3.382 -> 2.985 s; searches 392 -> 32,
  pipelines 448 -> 88, with unchanged row fetch counts. One reader is 3.400 ->
  3.456 s. Fixed-1K-call writes are slower in this diagnostic (7.973 -> 8.648 s);
  that observation is retained. Ten-K calls are 1.817 -> 1.785 s. These are
  different input/timing conditions from the native benchmark and not production
  throughput or a guaranteed speedup.
- `BenchmarkIngestColumn/per-column`, three samples per revision: median
  414716 -> 155590 ns per 10K exact integer values, 96 B / three allocations
  unchanged. This measures conversion CPU only, not complete managed writes.
- The matched Python/C-ABI runner `managed_followup.py` executes six measured
  repetitions after warmup for each managed read case (one/four readers,
  512/4096 batches) and three for each write case (SQL/Arrow one/four writers at
  1K calls, plus separately labeled Arrow 10K call ceilings). Base and candidate
  use separate processes and fresh private work directories, sequentially on
  the same verified Software fixture. Exact source/library versions and hashes
  are retained; both library hashes were rechecked after timing.

All 104 native trials completed and passed full value/type/multiplicity checks:
56 reads and 48 writes including warmups, zero conflicts or retries. Four-reader
4096-batch median: 1.874 -> 1.559 s, about 17% lower elapsed time. One-reader
control: 2.234 -> 2.244 s. At 512 batches, four readers are 1.859 -> 1.583 s.
The median per-trial maximum worker RSS at 4096 batches increased from 213.5 to
272.9 MiB for four readers; the one-reader control was 273.2 -> 273.0 MiB. Larger
pages exchange bounded additional resident memory for fewer round trips. This
metric excludes the parent and is not total application memory.

Fixed-1K-call native writes remain broadly similar: Arrow one writer 7.949 ->
7.888 s, four writers 17.292 -> 17.630 s; SQL controls 7.851 -> 8.122 s and
17.743 -> 18.127 s. No general managed write-throughput improvement is claimed.
The larger-call control is a separate existing usage option: candidate Arrow
one writer 1.621 s and four writers 2.339 s. Its publication units differ from
the fixed-call benchmark; the driver does not automatically combine calls.
The streaming example in docs/snapshots.md was verified through Python/C-ABI:
one append call consumed ten 1K input records, and all 10K x 105 values, types
and multiplicities matched. The first standalone smoke configured its TLS
environment in-process and could not trust the CA; it was relaunched with
the verified environment supplied at process start. Certificate verification
was kept enabled, and no data mutation occurred in the failed setup.

Native timing includes fresh workers/imports, source conversion/key or SQL-literal
construction, connections, complete fetch/IPC or write/publication, and shutdown.
Existing-table setup and full validation are outside timing. No tests/profiles
ran concurrently with native measurements. Shared workstation and sequential
base-then-candidate ordering limit causal inference; distributions and all samples
are retained. No measured attempts were discarded.

## Artifact checks, review and cleanup

`managed_followup_audit.py` verifies revision/version, data/configuration, exact
repetitions, call-size separation, complete output validation and summary
arithmetic for 104 native trials, plus 32 diagnostic trials and six microbench
samples. Missing native trial, incomplete write, mixed call size, wrong revision,
altered median and missing diagnostic samples are rejected. All historical
audits pass; old evidence bundles have no diff. The generic harness's supported
path declaration is retained as framework metadata; measured paths are labeled
managed SQL/Arrow. Observations are unchanged.

Two focused harness tests, CLI help, py_compile, gofmt, relative documentation
links, privacy scan and `git diff --check` pass. Existing assertions and skips
are preserved. Self-review per REVIEW.md covered correctness/lifecycle,
ownership/privacy and evidence/scope: exact typed conversion, nullability and
fallbacks, immutable candidate ordering/identity/visibility, cap/batch/tail bounds,
Arrow ownership/cancellation, retained index/ACL/pin proof and uncertain-outcome
handling. No format or atomicity change, row-writer pipelining, weakened readiness
proof, public driver profiling option or automatic statement/input retry. This is not
human maintainer approval.

Removed only the task-owned server, volumes and network; absence verified.
Removed its generated test environment file. Operational identifiers, credentials,
raw profiles/logs, typed input and licenses remain private. No 100M, physical
Flex, Flight SQL, creation or unmanaged/native-client performance rerun is claimed.
