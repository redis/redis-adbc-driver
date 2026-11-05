# Compaction indexes from exact output directories

Date: 2026-11-05. Production parent: ce36c86. Status: pre-copy index implementation and focused recovery checks pass;
broad snapshot/native/performance verification pending. See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and [the rejected polling experiment](compaction-readiness-polling.md).

The six-process native comparison finds no write gain from faster readiness
polling. A controlled selective job still spends substantial time waiting for
initial index construction. Ordinary sealed outputs already create an empty
index and populate it from their registered exact directory. Background
compaction instead starts a database initial scan even when its selective output
contains only a few row images. Avoiding that scan is the next optimization;
shorter observation intervals do not remove it.

The new `TestSnapshotCompactionBuildsIndexFromDirectory` fixture covers sparse
updates with a retained tombstone and dense multi-batch output. It runs real
compaction, counts native index creations through passive hooks and requires one
final index with no initial keyspace scan. Independent complete final fetches
check every ID, exact BIGINT above 2^53, payload and NULL. Its first sparse run
fails on the current implementation in 0.41 s because one initial scan starts.
A second run completes both independent final fetches, then fails the no-scan
assertion for both sparse and dense cases. The regression is now paired with a pre-copy empty-index implementation; neither
is included in ce36c86. Broad acceptance checks and native measurement remain
required before claiming the optimization complete.

Implementation must preserve these properties:

- Copy completion, immutable output identity and source lineage remain prerequisites.
  A missing row image must fail, never be recreated as a row-ID-only HASH.
- Index population must use bounded exact-directory pages and bounded native
  notifications under the real compaction lease, not an unbounded call or a
  foreground staging-writer identity.
- Preserve the existing background native readiness contract: exact index
  prefix/cardinality, no failures, completed indexing and canonical complete row
  images from the directory-backed atomic copy batches, including tombstones.
  Directory size alone does not prove Search visibility. Ordinary sealed-output
  row-ID query proofs and their typed permission fallback remain unchanged.
- Index creation, population/checkpoints and installation must survive Close,
  lease expiry and another worker's takeover without repeating SQL or consuming
  input. Unknown notification replies resolve durable outcomes; do not replay
  notifications on an uncertain result. Partial runtime outcomes need an explicit
  discoverable repair state and must never advance to installation.
- Existing protocol-15 jobs with initial-scan indexes must remain recoverable.
  Older workers must either finish safely or decline new incomplete work; they
  must not publish partial indexes or strand candidate admission. Compatibility
  needs actual preserved-native handoff tests, not only struct decoding.
- Preserve write-only index compatibility through a proven native permission
  refusal path. Do not select a fallback from transport errors or warnings, or
  broaden ACLs. Any replacement index remains private and reference-owned.
- Keep foreground writer yielding, the eight-segment gate, 16-generation capacity,
  source/output references and collection fencing unchanged.

Before retaining the change, exercise lost replies, partial runtime errors,
stale owners, multi-batch continuation, cancellation, exact rows/tombstones,
existing job recovery and actual older-native workers. Then run complete scoped
snapshot/native checks and the unchanged isolated six-process native comparison.
Current-source Cluster/HA/persistence and reader-principal gates remain separate
required work; no performance or compatibility completion follows from this
regression alone.


## Implementation refinement and focused results

A separate post-copy population cursor/journal is unnecessary. Before the first
copy batch, create the fixed output index empty with SKIPINITIALSCAN. Existing
atomic copy batches register directories and HSET complete immutable-source
images, naturally populating that index while committing their durable source
checkpoint. CopyPending still blocks phase advancement and repair still reads
the immutable source. No row-ID-only notification, new job field, storage version,
permission requirement or alias is introduced.

The server admits early CREATE only for the active compaction owner in copy
phase, with no source progress or pending batch, SKIPINITIALSCAN, the exact
registered output prefix/data ID, and an explicitly declared empty output
directory. The normal pre/post distributed Search fences remain. A successful
CREATE with a lost reply, or cancellation before copy, leaves rows/checkpoint
unchanged; the next owner observes and reuses the existing index without another
CREATE. Legacy checkpoints, including a legacy per-row write before its first
checkpoint, retain the initial-scan fallback when their unindexed directory is
already populated. The final index/install phases and readiness proof remain
unchanged, preserving write-only compatibility and older worker behavior.

The no-scan sparse/tombstone and dense final-row regressions plus atomic-copy
lost replies pass six race events (root 3.827 s). Additional legacy partial-copy,
interrupted creation and stale/nonempty/advanced/no-SKIP admission cases pass 14
events (root 6.638 s). Actual preserved 8d9f02b native workers publish preindexed
full-source jobs handed off during copy, index and install, before performing
12 non-replayed UPDATE RETURNING operations and checking all 257 final exact
BIGINT rows. Existing selective older-worker decline/recovery also passes at the
hard segment limit. Combined old-native/atomic/owner-fencing/Close/writer-yield
selection passes 28 race events, no skips/failures, root 19.593 s. These selections
overlap; their counts are not an independent total.

Owned Software 8.8.0, verified strict TLS, three primaries behind its proxy,
active/effective 4x QPF and noeviction. The native child uses the existing pinned
binary hash, not a model of its behavior. Full current snapshot/native checks,
matched sustained-write measurements and hosted validation remain pending.
