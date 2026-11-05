# Compaction indexes from exact output directories

Date: 2026-11-05. Production parent: ce36c86. Status: local full snapshot/native checks and matched performance comparison pass;
full ordinary/hosted/platform parity remains open. See [intent](intent.md), [spec](spec.md),
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
binary hash, not a model of its behavior. Full local snapshot/native and matched sustained-write results follow below;
hosted/platform validation remains separate work.


## Broad checks and dense attribution

At implementation 50d6b84, unit race checks pass 1,101 events / 972 expected
integration/profile skips, root 53.043 s. The complete snapshot race selection
passes 426 events / 25 explicit skips, root 722.593 s, including actual preserved
older-native cases. The 100,001-row compaction case passes in 311.570 s; its
preceding worktree checkpoint took 174.850 s. Those whole-test times include SQL
setup and final verification and do not isolate compaction performance.

A new opt-in dense mode in the existing phase fixture separates setup/final
checks from copy/index/install and pacing. An isolated preceding-production
checkout and current implementation use identical test bytes, race mode and
verified fixture, sequentially. [Raw observations and hashes](compaction-preindex-dense-profile.json)
record 20,000 rows, three dense updates, a full-source merge and 20,000 unrelated
rows; every final exact BIGINT/payload and unrelated count/bounds is checked.

| Instrumented phase | Preceding | Pre-copy index |
|---|---:|---:|
| Complete job ms | 9,892.944 | 6,559.856 |
| Pacing/wait ms | 5,730.918 | 1,736.174 |
| Copy steps / call ms | 314 / 3,839.774 | 314 / 4,600.116 |
| Index steps / call ms | 6 / 122.672 | 1 / 7.643 |
| Install steps / call ms | 1 / 22.737 | 1 / 21.538 |

Copy call time includes early index creation and increases; eliminating the
initial-scan waits improves this controlled full job by about 1.508x. This is
one instrumented Go observation per source, not statistical native throughput or
CPU attribution. It does not attribute the whole large-test timing difference.
The default sparse profile behavior and its one-minute budget are preserved;
the optional dense full-copy mode has a two-minute job budget. Native validation
and sustained-write results follow below; the profile is not substituted for them.


## Exact native validation and sustained comparison

The versioned 50d6b84 library SHA-256 is
`9471683d6c9b73be51ed5021d801cedec79ff1b49a5479d108809cb8890739a4`.
Its complete native ADBC suite passes 565 tests, eight capability skips and three
subtests, 87.34 s, after Go database processes terminated and the owned fixture
was reset/reverified. Python 3.14.10, PyArrow 26.0.1, manager 1.13.0.

The unchanged native harness then runs six isolated processes: preceding
snapshot d1d2778, pre-copy 50d6b84, mutable f4d5b78, then reverse order. Each
process verifies Software/effective 4x QPF/noeviction, performs 64 alternating
UPDATE RETURNING statements on two 20,000-row tables, and independently checks
every return and all 40,000 final rows. Caller retries are zero. Setup and final
verification are outside timing; no local builds, tests or database probes
compete during measured processes. This is synthetic load, not ClickBench.
Benchmark Python 3.13.16, PyArrow 26.0.0, manager 1.13.0.
[All samples, generic machine/settings and exact hashes](compaction-preindex-comparison.json)
retain each actual source and library. Apple M5 Max / 18 logical host CPUs /
36 GiB host memory; Docker resource allocation is recorded in the raw data.

| Source | Median ms | p95 ms | Max ms | Updates/s |
|---|---:|---:|---:|---:|
| Preceding snapshot d1d2778 | 65.020 | 2,940.930 | 2,990.632 | 2.783 |
| Pre-copy index 50d6b84 | 34.720 | 67.529 | 81.001 | 25.014 |
| Mutable f4d5b78 | 5.674 | 7.755 | 19.283 | 167.631 |

Measured throughput improves 8.988x and median latency improves 1.873x against
the preceding snapshot build. Two pre-copy runs measure 25.407 and 24.634
updates/s; the preceding runs measure 2.781 and 2.785. These are two isolated
runs per source, not a statistical or cross-workload performance guarantee.
Mutable throughput remains 6.701x higher and pre-copy median is 6.120x mutable.
Full performance parity remains incomplete. Keep the implementation based on
these exact-row/native/recovery results and measured improvement, while
continuing foreground metadata/index overhead work, reader authorization,
public process/input resume and the current platform/Cluster/recovery matrix.
The complete current ordinary Go suite and fresh hosted checks remain separate
validation work; prior 4e0899b hosted ordinary passes do not cover this change.
