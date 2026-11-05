# Selective compaction checkpoint

Date: 2026-11-05. Parent: be0ed4d. Status: implementation and scoped recovery /
actual older-worker checks pass; final combined validation and comparisons pending.
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
partial-tombstone recovery. Full validation of this additional fence is pending.

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
owner-reclamation change. Final combined snapshot/unit/native checks and fresh
both-order comparisons remain pending. The preceding 219.95x throughput gap is not a
measurement of this source. Index construction retains its existing initial-scan
path; the prior profile's inactive waits remain an independent cost. Reader
isolation/authorization, public process/input resume, current hosted/Cluster/HA/
persistence and complete feature/performance parity remain open.

Author self-review using REVIEW.md checks ordered range and lineage validation,
tombstone visibility, immutable checkpoint identity, source/output reference
ownership, cancellation and late-owner fences, and old-native job handoff.
The independent private-work lifetime regression was corrected by limiting
early reclamation to pre-clone reservations; its assertion remains unchanged.
No ACL grant, external-data ownership, protocol version, segment limit or
uncertain-operation replay was broadened. This is not maintainer approval.
