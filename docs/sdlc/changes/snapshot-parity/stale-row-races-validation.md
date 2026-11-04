# Native stale-row publication races

Date: 2026-11-04. Parent d794494. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The previous fresh ordinary continuation passed 52 events, then stopped at
TestSQLDropDuringCreateTableAs (root 29.457 s). Its legacy first-pipeline threshold
never runs the competing DROP at the native 256-row acknowledgement boundary.
The restored cases now inspect a real private directory and first/last stored
images after one acknowledged batch, then execute the competing public change.
They do not modify published HASHes or bypass fences by changing metadata age.

A CTAS destination is private and invisible. Another connection can create,
DROP and recreate that logical name with an independent replacement row; the
original CTAS must return typed I/O/SQLSTATE 40001 without changing the winning
root or leaking its output. INSERT SELECT starts from an existing target,
then conflicts after another connection drops and recreates it. An independent
old pin retains the exact original row through DROP/recreation; original source
storage retires only after that pin releases.

Arrow create and append span two records and also conflict after an acknowledged
private batch. The shared input helper tracks Next/record consumption and rejects
repeated consumption. A separate two-small-record case races after a real
128-row record. It requires serialization refusal and private reclamation rather
than injecting late raw HASH writes into an old published prefix.

Temporary CTAS is private too: the owning connection creates, drops and recreates
a competing binding in its own namespace during the outer statement. That
replacement survives the refused outer publication. No other connection gains
access to the owner's temporary namespace or pretends that an unpublished
descriptor can be swept as a published table.

Each replacement is checked exactly, then dropped/recreated and filled from the
source. Every resulting row must have its independently expected ID and NULL
note, with no old private values. Refused generations must lose rows, directories,
ownership and any working/final index. The identity test uses canonical unique
physical layouts across repeated DROP and logical rename, without fabricating
legacy name counters or claiming an earlier-format migration path.

## Validation and review

Owned strict-TLS Redis Software with noeviction and verified effective active
4x QPF. All nine focused race test events pass, zero test skips/failures; root
12.138 s. This includes the existing SameKeys unit case. The CUDA package has no
applicable selected tests and is skipped. Mutating selections run sequentially.
Compilation, formatting and diff whitespace checks pass.

Reviewed real acknowledgement/image checks, private destination visibility,
callback hook isolation, typed serialization refusal, unchanged winning roots,
complete replacement values, one-pass Arrow consumption, temporary ownership,
old-reader retention and reference-aware reclamation. No production, protocol
or ACL changes. Author review is not independent maintainer approval. Shared
legacy helpers still serve the remaining TRUNCATE/UPDATE race fixtures, which
need their own native conflict coverage. Full ordinary/platform, current-protocol
Cluster recovery, restricted-reader integration, public input/process resumption,
sustained compaction and comparative performance gates remain open.
