# Redis ADBC driver

Go implementation of SQL and Arrow bulk ingestion over Redis HASHes and Search,
built on driverbase-go. Read [the SDLC index](docs/sdlc/README.md) and the relevant
feature documentation before changing behavior. The module is in `go/`, not the
repository root; its toolchain is declared in `go/go.mod` (currently Go 1.26.10).

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
  policy declaration. Use a dedicated Compose project/ports for concurrent work.
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
  protocol version 14 rejects earlier draft formats; there is no disable switch
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
  ALTER stages private
  replacement layouts. Temporary descriptors participate in manifest references;
  liveness is global and Close/sweep publish descriptor removal. Application
  HASH adoption and foreign indexes are managed copies with explicit refreshes;
  never modify their external sources. Live partition sets retain a shared producer generation; retained
  production resolves segmented visibility under one coordinator pin.
  Preserve their server-side writer fences, reader pins and reclamation protocol.
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
  Catalog manifests share immutable row-version segments. DML allocates a new
  segment before planning logical row keys. Resolve newest versions/tombstones
  before predicates, aggregates or limits. Search candidates require newer-directory
  visibility checks. Server reducers and ordered scans require proven immutable
  per-segment masks (including tombstones); merge globally before OFFSET/LIMIT.
  Private outputs stay unindexed until final sealing; staging SQL reads exact
  directories. Build one final index and wait for readiness before publication.
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
  across expired worker leases. Installation grafts only the original prefix and
  respects write intents plus its short admission gate. Close/join workers before
  closing connection resources; old segments stay pinned. Never DROP/TRUNCATE shared data directly.
  Reclaim manifests separately from segment rows/indexes/directories using atomic
  reference checks. Explicit sweep previews must suppress connection-side GC;
  report confirmed reclamation and errors through the same ownership protocol.
  Preview covers currently zero-reference data and reports pending metadata;
  preserve pins/writer references and never infer ownership from prefix scans.
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
