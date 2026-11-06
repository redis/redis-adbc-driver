# Selective compaction checkpoint

Date: 2026-11-05. Implementation: 820ccfd; parent be0ed4d. Status: complete local
snapshot/unit/native selections pass; sustained performance parity fails.
See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[the selection design](selective-compaction-design.md) and
[the preceding measured phase costs](compaction-phase-profile.md).

## Behavior and ownership

Source selection preserves a much larger older prefix when at least two newer
segments can be merged. It chooses the earliest split where the older physical
row estimate exceeds four times the newer estimate, recovering the most slots
among qualifying splits. Balanced or overflowing/invalid estimates retain a full
merge. The eight-segment limit is unchanged. The job records a contiguous source
position and a descriptor/reference list containing only that selected range.
Claiming checks the range against the durable candidate; current-generation and
installation checks require the same ordered range and lineage. Installation
retains both older segments and every newer tail publication.

Partial-range output keeps winning tombstones, including a tombstone-only HASH.
Dropping them would resurrect rows from the preserved base. Full oldest-prefix
merges can still discard tombstones. Image limits, atomic outcomes/checkpoints,
pending-batch repair, lease/phase fences and source/output references remain.
Metadata checkpoints cannot change the recorded source position or its deletion
policy. Physical directory/index counts include retained tombstones; visible
SQL results resolve all versions before predicates, aggregates and limits.

A further regression reproduces checkpoint replacement of the ordered source
references in 0.170 s. Checkpoints now also compare the complete source
descriptor, ordered references and original queue identity, while normal
decoded/encoded checkpoints remain renewable. JSON object key ordering is not
used as an identity proof. Source position alone did not protect these fields.
The focused combined race selection passes ten test events, no skips, root
4.069 s, including five refused identity changes, a normal checkpoint and
partial-tombstone recovery. The complete final selection below also covers this fence.

A selective claim retains its durable queue candidate until publication updates
or removes it, preserving FIFO age and conditional candidate updates. Older
protocol-15 workers compare the selected descriptor against the oldest current
prefix and decline the job before copying/installing it. Retained eligibility
lets them discover a full merge even at the hard segment limit. The optional
job field does not change table/row/manifest storage protocol 15. No migration,
ACL expansion, protocol-16 activation, new user option or SQL/input replay occurs.

## Reproduced cost and current scoped evidence

The pre-change 20,000-row fixture fails after rewriting 20,000 images and reading
19,999 unchanged base HASHes to consolidate three one-row updates. Both new
20,000-row update/deletion cases now pass with bounded copied images/base reads
and a preserved base plus one overlay. They fetch and independently compare all
20,000/19,999 final rows, exact BIGINT values and every NULL/string payload.
The deletion case also requires a retained physical tombstone. Three test events
pass, no skips/failures. These are work/correctness observations, not throughput.

The existing compaction/recovery selection passes 52 race test events, zero
failures and one opt-in profile skip; root 157.066 s. Fixtures designed for
64-row checkpoints, byte limits and a 100,001-row copy now use genuinely large
overlapping inputs. Their copy-count/byte/checkpoint and final-value assertions
remain unchanged. Early attempts exposed old small-fixture assumptions (including
an out-of-range preflight-test slice); those failures are retained privately,
not converted to skips. The streaming reclamation test now requires referenced
base rows/indexes to survive and actually obsolete selected segments to be
reclaimed; at least one obsolete segment must be exercised.

Two additional race cases pass, no failures, root 2.783 s (the separately gated
older-binary test skips in that selection). They verify middle-range installation
with newer deletion/insertion and row-ID sequence preservation, old-reader
retention/reclamation, immutable source-position checkpoints and replacement
worker repair after a real Lua failure following the first HASH mutation.

## Actual older native worker

The preserved 8d9f02b native driver, SHA-256
`a36f1eddbf40dcda8f5e6a083f8540232201e49f3aa525c4d1b30eef8e97bce7`,
executes in a separate Python/ADBC-manager process. Three cases hand it a
selective job at copy, index or install, each with an actual eight-segment table.
After cooperative release it must decline that job, discover remaining work,
and finish 12 UPDATE RETURNING operations without caller retries. Every return
and all 256 final rows are checked, including exact integers beyond 2^53 and a
deleted row that must not reappear. The new worker then verifies current values
and complete reclamation of the declined output. All three cases plus their
parent pass. This proves the tested binary's behavior, not every historical
protocol-15 binary or actual crash/failover recovery.

All database checks use the owned Redis Software 8.8.0 strict-TLS fixture with
verified active/effective 4x QPF and noeviction, three primary shards behind its
proxy, macOS ARM64 and Go 1.27.3. The old native probe uses the installed Python
3.14.10 / PyArrow 26.0.1 / ADBC manager 1.13.0 runtime. Operational paths and
credentials remain private. The old native probe publishes outcome counts only;
subprocess diagnostics stay in private local logs.

Before the subsequent owner-reclamation fix, the complete snapshot race suite
passes 404 test events with 24 skips (root 411.133 s), including the actual old
native worker handoffs. Unit-only race checks pass 1,097 events with 956 expected
integration/opt-in skips (root 55.490 s). These runs do not validate the later
owner-reclamation change. Fresh both-order comparisons are recorded below.
The preceding 219.95x throughput gap is not a
measurement of this source. Index construction retains its existing initial-scan
path; the prior profile's inactive waits remain an independent cost. Reader
isolation/authorization, public process/input resume, current hosted/Cluster/HA/
persistence and complete feature/performance parity remain open.

## Complete local checks at 820ccfd

The final complete snapshot race selection passes 406 test events, 24 explicit
skips and zero failures; root 418.647 s. This includes actual older-native worker
handoffs, new immutable-source checkpoint checks, ownerless pre-clone reservation
reclamation, unchanged independent private-copy retention, and existing large
copy/import/rewrite/resource tests. The command is `make -C go test-software`
with `TEST_ARGS='-race -json -failfast -run Snapshot'`, the disposable snapshot
URI and preserved older native library/Python environment supplied privately.
The 24 skips retain nine absent Cluster fixtures, six reader-model cases, six
permission/recovery cases and three opt-in profiles. None proves those features.

Unit-only race checks (`env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -race -json
-count=1 -p 1 ./...` from go/) pass 1,097 events, 958 expected integration/opt-in
skips and zero failures; root 54.076 s. The versioned `make -C go build` library,
SHA-256 `7f703b8241e77a4f1035228fc0008032c92869aefa08a8ca178466025dd8a7b1`,
passes the complete native ADBC `pytest -q -ra tests/` selection: 565 tests and
three subtests passed, eight capability skips, zero failures, 90.68 s. Skips
retain unimplemented connection/ingestion capabilities, table-type filtering,
secondary catalogs and transactions. Python 3.14.10, PyArrow 26.0.1 and ADBC
manager 1.13.0. The earlier two-test close/cancellation check overlaps this suite.

Database-mutating suites ran sequentially after their preceding handles reached
terminal state; unit-only checks could run concurrently. No current full ordinary
Go, platform, actual Cluster API, HA/persistence or restricted-principal pass is
claimed. The [hosted record](atomic-compaction-hosted-validation.md) preserves
failed be0ed4d checks and links fresh runs rather than treating local checks as
hosted success. `git diff --check` and new evidence-document links pass.

## Fresh sustained comparison: parity still fails

The six fresh processes run in order: preceding snapshot be0ed4d, selective
820ccfd, mutable f4d5b78, mutable, selective, preceding snapshot. Each process
performs 64 alternating UPDATE RETURNING operations on two 20,000-row tables,
with no caller retries. Every return and all 40,000 final rows are independently
checked. Drivers and the unchanged benchmark harness are hash-verified before
each invocation. Timers include complete publication and RETURNING fetch;
setup/final full verification are excluded. No concurrent local builds, tests or
database probes run during timed intervals. This synthetic workload is not
ClickBench. [All raw samples and provenance](selective-compaction-comparison.json).

| Implementation | Median ms | p95 ms | Maximum ms | Updates/s |
|---|---:|---:|---:|---:|
| Previous snapshot be0ed4d | 31.928 | 7,079.082 | 7,200.391 | 0.774 |
| Selective snapshot 820ccfd | 63.377 | 2,832.149 | 2,961.866 | 2.824 |
| Mutable f4d5b78 | 5.640 | 6.755 | 10.082 | 172.905 |

Selective compaction improves aggregate throughput 3.651x and reduces p95, but
**median latency regresses 1.985x** against the previous snapshot implementation.
Mutable throughput remains **61.217x faster**, and selective median latency is
11.237x mutable. The performance-parity gate fails. Both individual selective
runs show the throughput improvement (2.851 and 2.799 updates/s); these finite
workloads do not establish indefinite stability or all-workload performance.
The median regression remains unexplained by the separate compaction-only
profile and needs foreground/visibility/publication attribution.

All runs use the same owned Software fixture/settings above, macOS 26.7.2 ARM64,
Apple M5 Max, 18 logical host CPUs and 36 GiB host memory. Immediately after the
runs, Docker reports 18 CPUs and 25,421,033,472 bytes of memory capacity. Native
benchmark Python is 3.13.16, PyArrow 26.0.0 and ADBC manager 1.13.0. These generic
hardware capacities are recorded in the raw artifact; operational IDs stay private.

## Selective phase attribution

After all timed processes terminate, the unchanged opt-in real-step profile
passes three race test events, no skips/failures, root 12.526 s. It fetches and
checks every final row and the unrelated table. [Raw phase/call timings](selective-compaction-phase-profile.json).
The paced job takes 2,138.989 ms: two copy steps total 16.278 ms, four index steps
58.684 ms, one install step 29.010 ms and measured waits 2,029.484 ms. Omitting
the active five-millisecond delay still takes 2,105.318 ms, including 2,001.483 ms
of waits; copy/index/install take 16.192/54.278/27.171 ms. The one-image merge
removes the prior full-base copy, but inactive waits now dominate this controlled
job. It is a client phase profile, not CPU profiling or end-to-end native
throughput. The next compaction investigation is bounded polling while an owned
job waits for index readiness, without changing storage, leases or replay rules;
index construction and the separate median regression still need measurement.

Author self-review using REVIEW.md checks ordered range and lineage validation,
tombstone visibility, immutable checkpoint identity, source/output reference
ownership, cancellation and late-owner fences, and old-native job handoff.
The independent private-work lifetime regression was corrected by limiting
early reclamation to pre-clone reservations; its assertion remains unchanged.
No ACL grant, external-data ownership, protocol version, segment limit or
uncertain-operation replay was broadened. This is not maintainer approval.
