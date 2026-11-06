# Rekey Close at private-copy and publication boundaries

Date: 2026-11-04. Parent 0702b26. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 550-name ordinary continuation passes 40 test events and skips one
profile case, then fails TestSQLRekeyCloseDuringMove: the rename finishes before
the legacy copy-phase hook fires (root 11.009 s). Snapshot rekey uses bounded
private rewrites and atomic root publication, rather than mutable rekey jobs.

The three Close fixtures now exercise actual snapshot boundaries:

- Before publication, pause on the second real source page after a durable
  256-row private batch. Close must join the interrupted operation, return its
  closed-connection error, release its reader/writer state and reclaim private
  data. Exact original metadata and all 600 rows remain public; subsequent INSERT
  and an explicit rekey succeed, and old source segments are reclaimed.
- After Redis commits the new root, pause its reply and close the connection.
  Both an acknowledged reply and an injected lost reply must preserve success
  and the committed target. The latter resolves the durable outcome. A source
  counter requires exactly 600 rows in three pages, proving no rewrite replay.
  Old pinned source samples remain exact, and source segments are reclaimed only
  after pin release. A committed outcome record is valid durable evidence, not
  active private work; its reader lease must be released.
- Let a source callback ignore cancellation, then use a 150 ms caller deadline
  for Close. Close must report incomplete cleanup within the bound while keeping
  the unfinished execution registered. Atomically expire the owned private
  control records to model lease time passing without resurrecting a reader
  already released by its heartbeat. Collection reclaims private output and a
  fresh connection writes successfully. After the old callback resumes, it must
  fail closed, read no third source page, restore no private output, publish no
  target and leave no registered execution.

The optional uncooperative barrier affects only this fixture. Existing ALTER
cancellation, Close and lost-lease tests retain their cooperative defaults.
Publication hooks are installed on an idle connection before statement execution;
maintenance compaction uses its separate pool. No published HASH, index or table
metadata is edited directly.

## Validation and review

Owned Redis Software with effective active 4x QPF and noeviction verified before
testing; mutating selections run sequentially:

- Rekey Close plus existing ALTER cancellation/Close/lost-lease race selection:
  ten test events pass, no skips/failures; root 6.257 s.
- Final deadline case with explicit unfinished/completed execution registration
  assertions: one race event passes, no skips/failures; root 2.763 s.
- Formatting and diff whitespace checks pass.

Reviewed acknowledged batches, actual publication replies, durable outcome
resolution, original versus committed data ownership, inactive committed records,
cleanup deadlines, operation registration and stale execution fencing. No
production code or storage format changes are required. This author review is
not independent maintainer approval. These fixtures do not implement public
process/input resumption or prove full Cluster failover/persistence recovery.
Full ordinary/platform, restricted-reader integration and sustained/comparative
performance gates remain open.
