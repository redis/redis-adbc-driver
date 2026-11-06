# Comment metadata, rekey and publication conflicts

Date: 2026-11-03. Parent 508717b. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and fixture changes

The ordinary Software diagnostic with `-count=1 -p 1 -failfast -skip
TestACLAppIndexReader` reaches 420 pass events, ten capability skips and one
failure in 87.421 seconds for the root package. COMMENT metadata removal uses
an unscoped GET, which the statement-snapshot fence correctly rejects. The
assertion now inspects the same immutable definition under a pin.

The isolated comment family then exposes a legacy table-name physical-prefix
expectation. Forced rekey now requires a fresh canonical UUID prefix and index,
exact comments/values and unchanged pinned source images, followed by confirmed
reference-aware reclamation after pin release. An initial fixture revision
incorrectly assumed row ID 1 after TRUNCATE; it now obtains canonical IDs from
the exact pinned directory rather than assuming the sequence was reset.

A non-failfast inventory records six passes and three failures in 11.253 seconds:
the above row-ID assumption, mutable beginRekey setup and expected success for
every same-table concurrent writer. The remaining fixtures now stage real private
snapshot operations instead of invoking legacy mutable-move internals.

Three fully staged rekeys race later table comments, column comments and an
INSERT. Each stale publication must return 40001, leave the destination absent,
preserve exact pinned inputs and reclaim its private output/index. A fresh
explicit rekey then retains all intervening comments and both rows.

The concurrency test stages individual statements on the same table definition
before racing publication. Eight column comments compete with one ADD COLUMN;
exactly one succeeds and every other result must be 40001. Before any fresh
statement, information_schema, GetObjects and GetTableSchema must show exactly
the winning definition with no partial loser changes. Explicit fresh client
statements apply the known-refused changes. A second eight-way table-comment race
again requires exactly one winner, exact winner text and all existing column
comments plus the added column. No compound transaction or automatic SQL replay
is introduced. A serialization refusal is not suppressed as success.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites are sequential on macOS ARM64. No ClickBench run/reload. Operational
artifacts and credentials remain private.

- Restored comment/create-comment family with `-race -count=1 -p 1`: nine
  top-level passes, zero skips/failures; root package 15.449 seconds. It retains
  table/view remarks, replacement/rename, temporary isolation, errors, dbt-like
  flows and all three metadata interfaces.
- Unit-only packages: 923 pass events, 822 expected integration skips, zero
  failures. These skips do not establish server compatibility.
- GitHub returned no workflow runs for production head 508717b at inspection;
  no hosted success is inferred from that absence.
- Go formatting and diff whitespace checks pass. Production code and storage
  are unchanged; prior production/native evidence remains revision-specific.

The subsequent ordinary diagnostic, again excluding only TestACLAppIndexReader
on the command line, reaches 429 pass events, ten capability skips and one
failure; root package 136.186 seconds. It passes the restored comment family
then stops at TestSQLConstraintForms: the rejected-CREATE absence assertion uses
an unscoped legacy tableExists store. That fixture and later cases remain to be
restored. This is not complete ordinary-suite success.

## Review and remaining work

Author review covers canonical IDs, immutable source retention, private output
ownership, exact winner/loser outcomes, release/abort cleanup, per-statement
autocommit boundaries and explicit client continuation after known refusals.
This is not independent maintainer approval. Full ordinary/current Cluster,
platform/recovery, restricted reader ACL, process resume, sustained compaction
and broader performance gates remain open. The pending protocol-16 reader design
is not activated.
