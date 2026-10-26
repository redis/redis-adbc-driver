# Evidence and review

2026-10-21 America/Los_Angeles. Author: Codex; requestor: Albert Wong.
Base `9510b9d` includes merged #424 (`5c25bad`). This implements public retained
execution for #359/#360/#362 and adds runtime-specific checks/measurements for
#363. See [intent](intent.md), [spec](spec.md), [plan](plan.md), the
[public contract](../../../retained-partition-results.md) and
[exact source/library metadata](evidence/metadata.json).

## Checks

| Check | Result / evidence |
|---|---|
| Final full configured Go suite, Go 1.26.6, `go test -count=1 -p 1 ./...` | All three packages passed against owned Redis 8.10.2 / Search 8.10.1; [log](evidence/go-full.log). This command does not enumerate feature skips. |
| Standalone retained/live/timeout regressions, `go test -race -count=1 -p 1 -run '^Test(Retained\|Partition\|ErrorHook)' -v .` | 77 top-level passes; three subprocess helpers skipped only in the parent (executed by subprocess tests), TLS-root case deferred to TLS run. Cap and safe policy mutation explicitly enabled only on owned fixture. [Log](evidence/standalone-race.log). |
| Three-primary verified TLS Cluster, Redis 8.6.2 / Search 8.6.0, retained/error-hook race suite | 56 top-level passes; three parent helper skips and standalone-only policy mutation skipped. TLS roots/hostname verification retained, RESP3, independent minimal-ACL workers. [Log](evidence/cluster-tls-race.log). |
| Final abort/connection-close boundaries on TLS Cluster | Five prepublication failures (cancel, cardinality, schema, index, view) and close-before-registration pass; [log](evidence/final-cancel-close.log). |
| Full native ADBC suite on final macOS ARM64 library | **375 passed, 8 skipped, 3 subtests passed**; [log](evidence/native-full.log), [JUnit](evidence/native-full.xml). Required partition gate: **13 cases passed**, no partition skips. |
| Final native retained tests on TLS Cluster | Three passed: complete global SQL matrix, typed replay/revoke after DML/DDL, planner death plus worker death and retry. [Log](evidence/native-cluster-tls.log). |
| Unit-only Go suite | All three packages passed; Redis integration skips are not deployment evidence. [Log](evidence/go-unit.log). |
| RESP pre-decode fuzzing | **175,044 executions, zero failures**, bounded 20-second run/two workers. [Log](evidence/resp-fuzz.log). Sampled evidence, not a safety proof. |
| CI coverage selector | Five tests passed, including retained native-test sensitivity. [Log](evidence/ci-matrix.log). |
| Resource options / policy rejection | Passed; invalid options leave defaults unchanged, persistent-quota policy excludes allkeys. [Log](evidence/budget-options.log). |
| Formatting / Python compilation / whitespace | `gofmt`, relevant `py_compile` and `git diff --check` passed. |

Native skips: unimplemented connection metadata/statistics/catalog capabilities,
two unsupported ingestion capabilities, and transactions. These remain explicit
upstream feature skips. The runtime uses the Makefile version linker flag and
macOS deployment target 12.0; a bare unversioned native build was not used.

The final full Go command set REDIS_URI to the owned loopback port 16485,
REDIS_PARTITION_CAP_TEST=1 and REDIS_RETAINED_POLICY_TEST=1. The policy test also
requires standalone maxmemory=0 and restores the prior policy; it cannot mutate
an arbitrary Cloud/shared deployment. TLS runs load URI/CA from private fixture
files without printing credentials. Test-only trust-root plumbing is enabled
only by explicit REDIS_RETAINED_TLS_CA_FILE, without editing system roots.

## Runtime coverage

Global sorts/limits/offsets, DISTINCT/DISTINCT ON, aggregates/HAVING/all-NULL
values, windows/QUALIFY, sets, joins, CTEs, permanent views and correlated/IN
subqueries run once and match fully fetched ordinary Arrow values/schema.
Native coverage includes int64 above 2^53 through MaxInt64, decimal, binary,
embedded NUL, NULL and empty results. Unordered ARRAY_AGG compares exact typed
element multisets because Cluster cursor arrival can vary across independent
executions. Explicit aggregate ORDER BY and committed replay retain exact order.

Capture tests cover actual large physical row IDs, missing-column defaults,
legacy metadata without migration, adopted/application/foreign HASH ownership,
foreign multi-prefix/filter coverage, binary key identity and budget rejection.
Public layout tests project arbitrary keys as BYTEA and verify source data and
index definitions are unchanged. Source schema/index/view changes between upload
and publication return an error, remove payload/manifests and refund byte quota.

Reader/storage regressions cover malformed descriptors, independent minimal ACL,
tenant separation, checksum/schema/count failures, chunk/manifest eviction,
expiry between chunks, cancellation, early release, Arrow ownership, quota races,
revoke/upload races and damaged job metadata. A real child process exits after
upload but before commit: there is no manifest, every upload has a bounded lease,
and its reservation remains accounted until explicit cleanup/expiry. Native
children exit after commit or die after partial consumption; fresh workers replay
identical data after source writes and DROP. No automatic SQL retries are added.

The final release gate requires the retained native cases. The minimum macOS
ADBC manager invocation now collects both live and retained test modules.
Hosted Linux/packaged/Cloud/Software results for this runtime are still required
before #363 can close; historical results do not validate this new code.

## Reproduced corrections

- A structured ADBC error contains a slice and cannot be compared with `!=` as
  an interface value. The Redis timeout hook panicked while rewriting such an
  error. Command/pipeline regressions failed before and pass after removing that
  comparison. [Before](evidence/hook-panic-before.log).
- JSON encoding an arbitrary byte prefix as a string replaced invalid UTF-8 and
  collided source identities. Definitions now hash byte-preserving components.
  [Before](evidence/binary-identity-before.log).
- Captured in-memory sources hid explicit __key/__rowid columns. Their physical
  source identity is now retained without adding system columns to SELECT *.
  [Before](evidence/system-columns-before.log).
- Explicit revocation of missing/corrupt job metadata previously left payload
  and quota. It now uses the authoritative expiry ledger and complete bounded
  chunk key space. [Before](evidence/revoke-metadata-before.log).
- A connection could close after the first retained chunk loaded but before its
  reader registered. The public path now checks closure after registration and
  releases the reader. [Deterministic before case](evidence/close-registration-before.log).
- Abort cleanup uses a fresh pool and closes it at its cleanup deadline, including
  when ordinary socket timeouts are disabled. No ordinary/shared pool is closed.

Development fixtures also needed Cluster-safe DUMP/RESTORE/DEL instead of
cross-slot RENAME, and DuckDB generated-expression labels were aligned by column
ordinal in the ClickBench oracle. Exact driver schema checks remain mandatory.

## Measurements, review and cleanup

[Repeated measurements](../../../../benchmarks/partitions/retained-2026-10-21/README.md)
include 168 complete live/retained synthetic reads and 90 reduced ClickBench
reads with exact oracles, actual/requested partition counts, workers, memory,
network and versions. Retained output adds coordinator/storage cost; no general
speedup is claimed. The 100k retained case explicitly raises transport/source/
working/work limits. An over-budget development run failed without publication;
its time is not reported as successful throughput. Each report has its measured
library hash. These measurements precede merge `1bea4aa` of main through
`944170f`; they do not measure the newer main scan/aggregation optimizations.
The post-merge compatibility run is recorded separately below.

Agent review followed [REVIEW.md](../../../../REVIEW.md): correctness/lifecycle,
then ACL/key/ownership boundaries, then specification/evidence. The reproduced
findings above are corrected. Resource accounting remains a documented logical
model, not process RSS or process-wide admission; source capture is not atomic.
Unsafe expressions, temporary sources, spill and source predicate/projection
pushdown remain explicitly unsupported. This is not maintainer approval.

All owned native fixtures were PID-verified before shutdown. The TLS Cluster's
exact container identity and ownership label were checked before removal. All
three private fixture directories, keys and passwords were removed; no paid or
Cloud resources were created. [Cleanup record](evidence/cleanup.json). Logs were
checked for credential-bearing Redis URLs before inclusion. Source/output
ownership checks remove only test-owned data and namespaces.

## Main integration

Main through `944170f` was merged as `1bea4aa`. Its aggregate fast paths
exclude captured in-memory sources. Retained publication and replay now honor
its optional `stream_batch_bytes` accounting, including nested types and one
oversized row for progress; worker slices still retain a bounded IPC chunk.
Focused accounting and reader tests pass. The native regression checks planner
and worker limits independently and is required by the partition JUnit gate.

Fresh-fixture validation exposed a fixture bug in legacy-layout coverage: a
first-use physical prefix can already equal the legacy prefix. The fixture now
avoids copying a key onto itself, removes its rows explicitly, and tests both
fresh and reused prefixes. Post-merge checks on runtime commit `f9f2dad` passed:

- Full configured Go: all three packages passed; [log](evidence/post-merge/go-full.log).
- Native ADBC: **380 passed, 8 feature skips, 3 subtests**; all **14 required partition cases** passed. [Log](evidence/post-merge/native-full.log), [JUnit](evidence/post-merge/native-full.xml).
- Retained/error-hook TLS race suite: **58 top-level passes**, three parent subprocess-helper skips and one standalone-only policy test. [Log](evidence/post-merge/cluster-tls-race.log).
- All four native retained cases passed on three-primary TLS using the explicit CA option. [Log](evidence/post-merge/native-cluster-tls.log). On macOS, SSL_CERT_FILE alone did not trust the fixture; explicit driver trust roots preserve certificate verification.
- CI selector: five tests passed. [Log](evidence/post-merge/ci-matrix.log).

[Source/library metadata](evidence/post-merge/metadata.json) identifies this run.
These two owned local fixtures were subsequently removed during #363 follow-up.
This earlier runtime run created no paid resources; later Cloud Pro validation
and verified cleanup are recorded in the
[deployment evidence](../retained-deployment-validation/evidence.md).
