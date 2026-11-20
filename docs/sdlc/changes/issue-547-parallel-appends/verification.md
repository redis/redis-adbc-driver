# Verification and review

Date: 2026-11-20. Initial investigation: `52b03c6f`; implementation rebased onto
`845c95b4`. Requestor: alberttwong. Author/reviewer: Codex (agent self-review).
Status: implemented and locally verified; human review and hosted CI pending.
No release or human approval is recorded.

[Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md)

## Environment and results

Dedicated Redis Software RAM and actual Flex fixtures used embedded Redis 8.8.0,
TLS/RESP3 and noeviction. `make -C go redis-up` provisioned RAM; the separate
Flex setup provisioned BigStore v2. Both setup paths and a subsequent management
read verified active scaling factor 4, six effective Search workers, mnp routing
and 32 connections. RAM had three primary shards; Flex had one. Client: macOS
26.7.3 arm64, Go 1.27.4, host Docker-published TLS ports. Other development work
shared the host/VM; measurements are not dedicated-host performance evidence.
Operational details, certificates, raw logs and credentials remain private.
Both temporary fixtures and their dedicated network were retired after checks.

Commands below were run from `go/`, with private setup-generated URI/trust and
the owned endpoint's verified persistence-policy declaration selected. Snapshot
suites used only their disposable `REDIS_SNAPSHOT_URI`, not a shared ordinary
suite database. All package selections used `-count=1 -p 1`.

| Check | Actual outcome |
|---|---|
| `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` | Passed all packages after the final error-classification correction. Redis-dependent cases skip in this unit-only pass; it is not server compatibility evidence. |
| RAM broad race selection: `go test -race -count=1 -p 1 -timeout=12m -run '^Test(Snapshot(ParallelAppend\|IngestAdmission\|IndependentPublication\|Catalog.*\|.*Compaction.*\|ArrowIngest.*\|.*Metadata.*\|Partition.*)\|ManagedStatementTimeout\|StatementTimeout)' .` | 447.357 s; failed only the three short/caller-cancel/caller-deadline Arrow timeout cases. This batch is not relabeled as green. It exposed the private append counter read returning a raw context error. |
| Final RAM targeted race selection after fixing that classification: `go test -race -count=1 -p 1 -v -timeout=5m -run '^Test(ManagedStatementTimeout\|StatementTimeout\|SnapshotParallelAppend)' .` | Passed, 18.605 s; 19 top-level passes and two top-level skips (standalone child helper and opt-in profile). Includes exact high int64 row IDs, intermediate pinned roots after GC, stream cancellation/failure, process isolation, SQL/DDL coordination, compaction pressure and all merged timeout regressions. |
| Earlier broad RAM race selection, before adding the timeout/partition selection: `go test -race -count=1 -p 1 -timeout=12m -run '^TestSnapshot(ParallelAppend\|IngestAdmission\|IndependentPublication\|Catalog.*\|.*Compaction.*\|ArrowIngest.*\|.*Metadata.*)' .` | Passed, 158.243 s. This earlier source result is additional evidence, not a substitute for the final targeted run. Opt-in diagnostic/profile tests skip without their gates. |
| Actual Flex, explicit stopgap: `go test -race -count=1 -p 1 -v -timeout=8m -run '^TestSnapshot(ParallelAppend\|FlexContracts)' .` | Passed, 44.632 s; 13 top-level passes and two top-level skips (standalone helper/profile). Runs existing Flex atomicity/index/publication/partition contracts plus the new cases. General ALTER and row IDs above the Flex ceiling are explicit subtest exclusions. This run preceded the final shared counter-error classification fix; final RAM tests also exercise the Flex driver overlap path. |
| `make build` | Passed after the final counter-error correction; native shared library built with the Makefile version linker flag. No C-boundary behavior changed. |
| Opt-in synthetic append profile, standard RAM and actual Flex | Both passed all six per-deployment trial/arm cases; each validates all 16,384 rows before/after maintenance. Exact revisions, metrics and limitations are in the [measurement report](../../../../benchmarks/parallel-managed-appends/report.md). |
| `gofmt`, `git diff --check`, changed Markdown local-target checks | Passed; final documentation/evidence files checked again before PR submission. |

No Valkey, ElastiCache or Memorystore tests were run. This is local Go/native-build
validation, not a newly published wheel, full hosted deployment matrix, or a
failover/restart/100-million-row result. Existing source-mode restrictions and
protocol-17 fresh-storage requirements remain.

## Regressions retained and diagnostic history

- Overlap fixtures block each stream on its second `Next`, after its first private
  Redis batch is acknowledged. All four must reach that point before any can
  publish; mere simultaneous BindStream calls cannot satisfy the assertion.
- Separate test-binary processes create independent drivers, pools and local
  schedulers. Exact successful source ranges and row-ID uniqueness are checked.
- The first metadata merge attempt moved a new native-value map into the original
  workspace. Later value GC shrank that committed root's native-key directory.
  Publishing the construction manifest in place fixes this; retained intermediate
  roots are now read again after subsequent appends and GC.
- The new twelve-stream pressure case exposed a pre-existing missing Flex flag
  on automatic maintenance stores. Workers now inherit the owning connection's
  Flex capability, and the actual Flex pressure case passes.
- Merged main's configurable-timeout regressions exposed raw context errors from
  the new private counter read. The retained upstream timeout cases now pass after
  transport/context-aware ADBC wrapping; no assertion or deadline was weakened.
- Admission test observers now recognize the new `admit_append` operation instead
  of waiting only for the old exclusive `admit_ingest` command. Their no-input-
  before-admission assertions remain intact.
- Initial 65,536-row profile attempts exceeded a 15-second maintenance helper
  budget. They are failed diagnostics, not completed measurements. The retained
  synthetic profile uses 16,384 rows and an explicit two-minute drain budget.

## Self-review against REVIEW.md

Correctness/lifecycle: checked leased peer admission, exact decimal high-water
propagation, per-stream row order/counter maxima, stale original/merge pins,
non-replayed unknown allocations/publication resolution, full target-definition
checks, TAG/NaN fact merging, segment reservations, compaction candidate identity,
reader retention and native metadata/data reference collection. The index sealing
and complete-row/directory/cardinality proofs remain in the existing pipeline.

Ownership/security: parallel eligibility excludes unmanaged/live/application-key
layouts. New coordination is bounded in the existing metadata control namespace;
source HASHes/indexes are never adopted or modified by these changes. Existing
native permissions, physical prefix identities and writer fences remain required.
No endpoint, credential, license or infrastructure identifier is committed.

Intent/evidence: verified four existing-table append peers across processes,
exclusive initial creation/replacement and SQL/catalog coordination, protocol-17
compatibility, explicit backpressure and the absence of automatic input replay.
The documentation reconciliation for [#552](https://github.com/redis/redis-adbc-driver/issues/552)
separates independent SELECTs, source partitioning, retained output and internal
workers, preserves historical release evidence, and marks unvalidated external
capture/deployment scope. Read features #549–#551 are separate PR work.

No additional actionable finding remains from this self-review. This statement
is not human approval. General speedup, global FIFO, arbitrary SQL conflict
freedom and all-mode partition parity are not claimed.
