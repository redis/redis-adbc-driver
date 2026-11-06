# Validation: independent metadata publication

Date: 2026-11-03. Parent checkpoint `03ad69f`. Status: locally verified;
[full parity](plan.md) remains incomplete.

## Contract and implementation

Storage protocol 10 adds sealed workspaces and metadata-only rebasing. Earlier
formats, including 9, are rejected without modification; there is no migration.
The resolver records original immutable metadata value owners, including absent
keys. Workspace creation inherits planning/admission dependencies captured before
reservation. Every changed logical key records its original version, including
blind writes; private reads keep that original dependency rather than depending
on the writer's own value. Dirty-table sealing does not enumerate unrelated
metadata, and merge construction uses a separate scope.

The first publication seals private outputs before checking the root CAS. The
uncontended path seals and commits in one server operation. On a known CAS
refusal, pin a latest generation and compare the original dependencies. Changed
read/target objects return 40001. Unrelated changes can merge: clone latest
bucket references, replace only changed logical entries with aliases of sealed
native values, and transfer that map into the original writer's root at commit.
Logical deletions remove the entry; bucket collisions preserve other updates.
Bucket/value/data reference ownership remains independent of workspace records.

Publication validates both the original writer pin/lease and the latest merge
pin/workspace/base. Root transfer, reference detachment, durable outcome and
pointer publication occur in one metadata-slot operation. Abandoned merge maps
retire normally. Sealed originals renew, survive reclamation until expiry, reject
mutations and can be aborted. A root change during merge repeats only metadata
construction, with at most eight attempts and context/lease checks. SQL, Arrow
input, defaults, volatile expressions, row staging and index creation are not
re-executed. Shared metadata objects/collections can still conflict; this is not
field-level merging or multi-statement transaction support.

## Regressions and corrections

Before: two UPDATEs to unrelated table descriptors in the same metadata bucket
caused the second publication to fail with 40001. After: both commit and preserve
both values; a pinned older generation retains its original values through GC.

Additional race-detector checks cover changed SELECT sources, dependencies read
before workspace creation, reservation after an unrelated commit, four simultaneous
writers, merging DROP with unrelated UPDATE, sealed renewal/abort/late mutation,
a further publication during merge, a lost successful merge reply, and independent
loss of the original/latest pin. Every failed pin check leaves the root unchanged.
Reference accounting includes private jobs/objects, queued objects and old roots.

The first focused implementation exposed nested acquisition returning the existing
scope with no new pin. Merge acquisition now explicitly removes only the snapshot
scope while retaining cancellation/deadlines. Dependency recording also includes
read-only values owned by their root; only private self-owned reads are skipped.
The existing same-target/expired-writer regression and new pre-workspace/source
checks pass after these corrections.

Native Arrow coverage uses two independent connections and a generator barrier
after each first batch, forcing both workspaces to consume input before either
can finish. Both appends succeed without client retries, generators each yield
exactly two batches, and complete reads preserve BIGINT values above 2^53.

## Checks

Owned Software proxy fixture: three primary shards, strict TLS, active verified
4x QPF and noeviction. Database-mutating suites ran sequentially. Native libraries
used `make -C go build` and its version linker flags on macOS ARM64.

- Complete snapshot race run: **105 pass events, seven expected Cluster API skips,
  zero failures**, 120.754 seconds.
- Supplemental both-pin fault run: parent and two cases passed under the race
  detector, 2.433 seconds; this test was added after the full run above.
- Complete Go unit package check: **895 pass events, 688 expected integration
  skips, zero failures**.
- Native C/ADBC snapshot validation: **12 passed**, 13.675 seconds.
- Native IPC module: **nine passed**, 1.10 seconds.
- Python syntax, gofmt and diff whitespace checks passed.

Author self-review checked immutable dependency versions, original versus latest
pin identity, sealed mutation fencing, per-key bucket merge/deletion, exact native
metadata preservation, reference transfer/GC, cancellation and uncertain outcome
handling. This is not maintainer approval.

No concurrency throughput benchmark or pre-snapshot parity claim is made by
these correctness checks. Bounded/resumable bucket construction, large atomic
operations, dirty-lineage compaction scheduling and sustained throughput, full
feature/CI/platform and final Cluster API/failover/AOF checks, broader comparisons
and owned cleanup remain required.
