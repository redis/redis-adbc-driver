# Bounded compaction throughput: specification

Date: 2026-11-20. Source: `1b5918e6`. Status: implemented, review pending.
[Intent](intent.md) · [Plan/evidence](plan.md)

## Contract

- One maintenance step may copy at most four existing 64-row atomic batches when
  selected source cardinalities total more than 1024 rows. Smaller selected jobs
  retain one batch; selective merges consider only selected sources. Clamp the
  cardinality estimate at threshold+1 without overflow.
- The first batch follows unchanged catalog pin/metadata/prefix validation.
  Before each additional batch, read the current catalog root. A changed root
  stops the burst; the next normal step acquires/validates the new root. An
  uncertain root read fails rather than assuming equality.
- Every batch retains its existing Lua token/lease/physical prefix/ownership and
  exact checkpoint guards, <=64-row durable outcome, 1 MiB multi-row image target,
  64 MiB single-row ceiling and atomic registration/images/checkpoint semantics.
  The byte target is per atomic batch, not an aggregate step promise. A step can
  copy up to 256 ordinary rows / four one-MiB batches, subject to its existing
  two-second worker context and the existing oversize-single-row exception.
- Check cancellation before every copy; stop on error, no checkpoint movement,
  phase transition or private-job retirement. If uncertain-reply reconciliation
  adopts a successor's durable token, stop: that token proves progress, not this
  worker's authority to continue. Never replay an uncertain batch.
- Durable job references protect immutable sources and output independently of
  the released short catalog pin. Root comparison is an early invalidation
  check, not a new atomic DDL fence; the existing race between pin validation and
  one private copy still exists. Each batch rechecks its job fence, and unchanged
  installation validates current source identity, lineage and newer tails.
- Four-segment trigger, eight-segment gate, eight-command prefetch ceiling,
  publication, GC, provenance/tombstones and reader references are unchanged.
  No on-disk format, Lua argument guard or rollback compatibility change.

## Acceptance and risk

Focused failure/recovery suites and bounded native 105-column control compare
one/two/four batches per step. Final complete rows/columns must independently
match expected values. Report server, QPF, transport and source-specific context.
The control is synthetic, not canonical ClickBench. Wide four-writer load,
full-scale backlog/cleanup, process CPU and storage attribution remain separate
acceptance evidence unless actually measured. Longer individual maintenance
steps may increase command/GC fairness pressure; the context and burst cap bound
work but do not establish universal speedup or optimal tuning.
