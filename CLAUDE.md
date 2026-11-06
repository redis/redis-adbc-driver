# Redis ADBC driver

Go implementation of SQL and Arrow bulk ingestion over Redis HASHes and Search,
built on driverbase-go. Read [the SDLC index](docs/sdlc/README.md) and the relevant
feature documentation before changing behavior. The module is in `go/`, not the
repository root; its toolchain is declared in `go/go.mod` (currently Go 1.27.3).

## Commands (from the repository root)

- Shared library: `make -C go build` (requires cgo and a C compiler; creates
  `go/build/libadbc_driver_redis.{so,dylib,dll}` for the host).
- HASH adoption CLI: `make -C go adbc-cli` (no cgo).
- Unit-only pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`;
  integration skips are expected and do not prove Redis compatibility.
- Disposable Redis Software with Search: `make -C go redis-up` (Docker Compose,
  curl, jq). Use a dedicated test database; suites modify data.
- Full default Software suite: `make -C go test-software`. It verifies QPF and
  noeviction through the management API before passing the retained-control
  policy declaration. Full package runs have a 30-minute budget (override with
  `GO_TEST_TIMEOUT`); individual statement/cancellation deadlines stay bounded.
  Use a dedicated Compose project/ports for concurrent work.
- Other verified endpoints: `REDIS_URI=... make -C go test`; Software requires
  `REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed` only after API policy verification.
- ADBC validation: `REDIS_URI=redis://localhost:6379/0 make -C go validate`
  (uv, Python >=3.13). Success is zero failures; report skips explicitly.
- Differential harness tests: `make -C go pgdiff-test`. Full comparison:
  `REDIS_URI=redis://localhost:6379/0 make -C go pgdiff` (Docker, uv).
- Format changed Go files with `gofmt`; there is no Makefile lint target.

## Architecture and invariants

- `go/pkg/` exposes the ADBC C boundary; `driver.go`, `database.go`,
  `connection.go`, `statement.go` implement handle lifecycle and options.
- `sql.go`, `query.go`, `eval.go` implement SQL; `store.go` owns the HASH/index
  layout. `ingest.go`, `arrowconv.go`, `stream.go` connect Arrow to execution.
- Row HASHes are authoritative. Index pushdown must preserve exact values,
  NULLs, ordering, precision and residual predicates; fall back when unsafe.
- Metadata uses `adbc:{meta}:` to share a cluster slot. Row prefixes are never
  reused. Preserve rename/rekey recovery and external HASH ownership rules.
- The 2026-11-02 user request authorizes managed-table statement snapshots and
  supersedes the earlier consistency-feature scope restriction. Multi-statement
  transactions remain outside this change.
- Autocommit only; Search is required; Flex is rejected. Do not imply full
  PostgreSQL compatibility or support for Valkey / Redis 7.2 derivatives.
- Managed-table statement snapshots are always enabled, using immutable
  generations in one hash slot; see [snapshots](docs/snapshots.md). Storage
  protocol version 16 rejects earlier draft formats; there is no disable switch
  or upgrade path. Row HASHes use escaped schema prefixes with `{meta}`; schema
  `adbc` uses `%61dbc` to stay outside the metadata ACL grant. Durable data records
  retain exact physical prefixes; descriptor registration and row/Search fences
  reject UUID aliases under another prefix. GC uses the registered prefix,
  including after schema removal. Managed imports retain original `__key` in a
  hidden provenance field, separate from physical IDs and public columns. Keep
  it in full-row DML, compaction, ALTER/rekey, late projection and partition reads.
  SQL-added rows have NULL provenance; refresh recaptures source rows/schema.
  Arrow replace must retain import origin/provenance while replacing public
  schema/data. Joined provenance uses qualified logical fields, with physical row
  IDs kept separate; keep it out of default star/NATURAL public schemas.
  Import source indexes serve capture only. Apply private index policy after
  native conversion, independently of source attributes/encodings and source
  membership; keep IndexColumns working with explicit source indexes.
  ALTER stages private metadata through the guarded descriptor pipeline.
  Definition-only changes share rows/indexes only when no conversion, field or
  index change, or row/constraint revalidation is required. Explicit rekeying
  still allocates a fresh layout. Other ALTERs stage replacement layouts with
  fresh indexes containing current fields and the deletion marker. A late index-create failure leaves only reclaimable private
  output. Constraint/schema races validate their pinned input and return 40001
  after a dependency changes; never silently revalidate or replay SQL. Regression
  fixtures stage private work and intervening publications rather than mutate
  published indexes or depend on legacy mutable-ALTER hooks. Temporary descriptors
  participate in manifest references;
  liveness is global and Close/sweep publish descriptor removal. Application
  HASH adoption and foreign indexes are managed copies with explicit refreshes;
  never modify their external sources. Capture imports into reference-owned
  private metadata before emitting destination HASHes, so overlapping application
  indexes cannot capture newly produced rows. Preserve first-observed source-key
  deduplication, binary keys and exact native values. Commit output images,
  capture consumption and the row-batch outcome in one script; preflight journal
  permissions/ownership before mutation. An incomplete import marker blocks
  further row batches and publication. Uncertain capture mutations abort without
  replay; uncertain output replies resolve the durable outcome. Journal pages
  do not imply process/input resume. Live partition sets retain a shared producer generation; retained
  production resolves segmented visibility under one coordinator pin.
  Preserve their server-side writer fences, reader pins and reclamation protocol.
  SQL sequence definitions are immutable catalog metadata. Their reference-owned
  native counters are an explicit volatile-value exception: exact int64 CAS
  allocation checks the reader/writer lease and current definition, retries only
  proven refusals and never resends an uncertain allocation. NEXTVAL/SETVAL gaps
  survive failed SQL; row/schema changes still publish atomically. Counter values
  remain collectible after their last catalog/pin reference is retired.
  Cluster API node clients need snapshot hooks, including newly discovered nodes.
  Run distributed Search schema commands outside Lua with pre/post admission;
  never replay uncertain operations. Metadata roots share immutable buckets and
  native values; preserve reference counts, presence proofs and deletion pruning.
  Buckets use ordered field directories and point lookups. Copy references in
  checkpointed batches (64 fields, 1 MiB target), retaining the partial target
  and construction source until completion/retirement. Incomplete builds cannot
  publish or accept metadata mutations. Never persist Redis SCAN cursors.
  Native table metadata objects retain their row segments; copy those edges before
  replacing shared metadata and update them atomically with descriptor SET. Work
  references hold only private outputs; durable jobs retain their own sources.
  Metadata dependencies include pre-workspace planning reads. Publication seals
  outputs before CAS/rebase; merge only dirty logical keys into a pinned latest
  root and validate both pins/leases. Same-target/read-source changes conflict;
  never replay SQL or Arrow. Sealed workspaces renew, reject mutations and retire
  on abort/expiry. Data ownership/counters live outside the bounded catalog; drain the durable
  zero-reference queue with writer fences intact. Never bulk-delete an old metadata workspace: later roots can share its values. Test Software nodes need
  stable private addresses across restarts; verify actual shard roles and Search
  recovery as well as active QPF before resuming SQL.
  RDB gates require completed snapshots strictly later than acknowledged probe
  writes at INFO's whole-second precision. Dirty each physical primary using
  verified management slot groups; promoted primaries can share an endpoint.
  Catalog manifests share immutable row-version segments. DML allocates a new
  segment before planning logical row keys. Resolve newest versions/tombstones
  before predicates, aggregates or limits. Search candidates require newer-directory
  visibility checks. Server reducers and ordered scans require proven immutable
  per-segment masks (including tombstones); merge globally before OFFSET/LIMIT.
  Tuple partitions establish visibility even when invoked independently of their
  coordinator. Compact authoritative scans carry exact row-ID bounds, including
  tails; a pushed Search range alone does not restrict a snapshot directory scan.
  Ordinary private outputs stay unindexed until final sealing; staging SQL reads exact
  directories. Build one empty final index for ordinary outputs, populate it from
  exact directories with bounded lease/ownership/identity/permission-checked
  unchanged row-ID notifications, then require readiness, zero failures, exact
  prefix/cardinality and a warning-free indexed-row-ID count before publication.
  Unknown notification replies abort without replay; never recreate a missing HASH.
  A typed native query-permission refusal retains write-only compatibility by
  constructing a new private index with the original initial-scan proof. Never
  select that fallback from an unknown transport outcome or a warning.
  Background compaction creates an empty final index before its first atomic
  complete-image copy batch, gated on an empty declared output directory and
  unchanged copy checkpoint. Existing copy outcomes/repair populate that index;
  keep the separate native readiness phase. Legacy already populated unindexed
  outputs retain their initial-scan recovery path. No new job format is needed.
  Track private tombstones when restoring a baseline's published overlay flag.
  No-op UPDATE/DELETE do not publish empty generations or build indexes.
  SQL/Arrow row writes and ALTER/rekey use bounded atomic batches (256 rows, 1 MiB target,
  64 MiB single-row ceiling) with one serialized durable outcome per workspace.
  Resolve uncertain replies by reading that outcome, never resending rows or
  consuming input again. Register directories before images so runtime failures
  remain reclaimable. Replacement may clear only registered private HASHes;
  preserve NULL/provenance and track tombstones plus acknowledged batch writes
  for no-op detection independently of unbatched quotas. Arrow conversion slices
  at 4096 rows; metadata/utilities retain their separate staging limits. This is not process resume
  of unread input or a persisted SQL plan.
  Exact-ID/private-index/mask-budget fallbacks must retain the full SQL predicate. Preserve exact, score-zero row-ID directories
  registered atomically by writer fences. Publish dirty-table compaction eligibility atomically with the root; never
  enumerate the catalog to find work. Preserve FIFO age, candidate identity and
  prefix proofs, conditional stale cleanup and durable job recovery. Background
  compaction starts at four
  segments; eight is an admission limit, never foreground compaction. Managed
  writers release full-lineage pins and wait for their worker before execution
  or Arrow consumption. Gate waiting honors cancellation; never replay inputs.
  Stores without workers retain explicit backpressure. Preserve
  durable copy checkpoints, lease/phase fences, and source/output references even
  across expired worker leases. Selective jobs preserve a much larger older
  prefix and retain winning tombstones; only full oldest-prefix merges discard
  them. Keep the exact source position/ordered identities immutable, and retain
  candidate eligibility until publication so older workers can decline a range
  job without stranding writers. Current-generation matching includes lineage. Compaction commits bounded complete images,
  directory registration and checkpoint together; incomplete batches retain a
  durable marker until repair and cannot advance through metadata checkpoints.
  Resolve unknown replies from durable progress without resending mutations;
  error logging must never overwrite completed progress with a stale job. Installation grafts only the recorded contiguous source range, preserving
  its older prefix and newer tail, and respects write intents plus its short admission gate. Close/join workers before
  closing connection resources; old segments stay pinned. Track execution
  completion separately from socket interruption: Close must leave the maintenance
  pool usable until private abort/pin cleanup finishes, with a bounded join.
  A stopped heartbeat does not prove server pin cleanup. Preserve failed release
  outcomes for bounded explicit retry, and share one attempt across concurrent
  callers without adding a second command to normal successful cleanup.
  Keep registration atomic with the closed flag and register stream owners before
  exposing an execution to Close. Preserve indeterminate-outcome diagnostics when
  mapping a canceled command to its connection-close cause. Never DROP/TRUNCATE shared data directly.
  Reclaim manifests separately from segment rows/indexes/directories using atomic
  reference checks. Before execution, exact typed capacity refusals can reclaim
  manifests and retry reservation with the same workspace ID, bounded by 16
  passes. Stop on unknown replies; do not clone metadata or consume SQL/input
  before confirmed admission. Competing writers can take a newly freed slot;
  preserve live owners and keep row/index GC outside this admission path.
  A concurrent collector's confirmed deletion can permit another bounded
  admission pass, but must not count as this collector's explicit sweep result.
  An ownerless pre-clone reservation is reclaimable before its work expiry;
  atomically fence it as deleting. Established private work retains its own
  lease/reference lifetime even after its base pin stops.
  Keep current/pinned roots protected and durable compaction ownership separate.
  Explicit sweep previews must suppress connection-side GC;
  report confirmed reclamation and errors through the same ownership protocol.
  Preview covers currently zero-reference data and reports pending metadata;
  preserve pins/writer references and never infer ownership from prefix scans.
  Substrait metadata binding uses a short-lived snapshot; execution rebinds
  under a fresh pin retained through the owned reader's lifetime. Keep exact
  layered visibility, upstream work budgets, buffered-batch lease validation and
  automatic cancellation cleanup. Installed plans retain no physical generation.
  Snapshot suites clear `REDIS_SNAPSHOT_URI` and its indexes: use a disposable
  database and run separately from ordinary suites.

## Verification and recurring pitfalls

The default development and test environment is Redis Software with **4x QPF**.
Every Redis Software test fixture must verify active scaling factor 4 and its
effective Search workers/routing before testing. `make -C go redis-up` configures
and verifies this for both new and reused databases. Do not support or test
Valkey, AWS ElastiCache, or GCP Memorystore.

ClickBench must always use Redis Software with verified 4x QPF, never Redis
Community. Restore an existing verified, compatible full-scale backup for repeat
query runs; fresh ingestion requires an explicit loading-performance objective.
Verify product distribution and effective QPF before timing. Historical results
from another distribution are not Redis Software performance evidence.

For explicitly requested Redis Cloud deployments, use Redis Cloud Pro.
Redis Software with 4x QPF remains the development and ClickBench default.
Use a dedicated non-production database, record deployment cost and ownership,
and clean up temporary resources after validation. Keep the existing OSS and
Redis Software compatibility checks.

Require RESP3 for single-endpoint and Cluster API clients. Do not support RESP2
or offer `protocol=2` as a compatibility option. Reject failed RESP3 negotiation.

Keep Go packages sequential (`-p 1`) against a shared Redis: opening a connection
can recover another package's abandoned move. Disable test caching (`-count=1`).
Software blocks some protocol ACL and command-counter checks; OSS covers them.
Do not weaken assertions or broaden skips to hide a regression. For a bug fix,
capture a focused failing case before the fix when practical, and retain it.
Run checks appropriate to the changed behavior and report commands, outcomes,
skips and limitations. Documentation-only edits need link/evidence checks.
Build validation libraries with `make build` or preserve its version linker flag;
upstream GetInfo validation rejects a bare build's development version string.
Finish Arrow fetch and validate results before claiming query success or speedup.
Preserve cursor cleanup, cancellation, Arrow ownership and worker-token release.
Use [REVIEW.md](REVIEW.md); record change-specific intent/spec/plan and evidence as
described in [the workflow](docs/sdlc/README.md). Historical measurements are not
current validation. Markdown instructions are advisory, not installed hooks.
