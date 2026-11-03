# Independent capture and private index policies

Date: 2026-11-03. Parent `7e09655`. Storage remains protocol 14; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Reproductions and implementation

Managed HASH import reused in-place index planning. An automatically selected or
explicit application index caused IndexColumns to be rejected, even though every
managed copy creates its own index. Direct HASH capture also left a decoded text
Boolean unindexed. A three-mode race regression reproduced both failures before
the correction. A separate foreign-copy regression reproduced a missing Boolean
NUMERIC attribute after importing YES through a source TAG index.

Capture now retains source membership/decoding separately from the private index.
The shared import path normalizes columns to native storage, then applies the
private index policy before creating/sealing the table. HASH adoption passes its
IndexColumns selection; foreign copies use default eligible columns. Renamed
columns use destination names. Nil IndexColumns selects normal eligible defaults;
an empty Go slice selects no public attributes. Intrinsic row/deletion/provenance
attributes still serve the snapshot protocol. Public index limits remain intact.

HashAdoption.SourceIndex (JSON source_index) reports the capture index, separately
from legacy AppIndex/Index fields. SQL uses its immutable private index. The
checklist identifies source selection and unusable candidates. A missing or
incompatible explicitly requested index yields a blocker report for dry run and
report plus error on Apply, with no publication. This restores useful diagnostics
instead of returning a generic error without its report. Source indexes must meet
the existing full-collection capture checks; filtered index views remain available
through foreign-index discovery. Source HASHes and indexes stay application-owned.

## Regression coverage

The complete TestAdoptAppIndex retains every original exact city/case/whitespace/
separator/empty-string/LIKE, numeric filter, aggregate, grouped and top-K result
assertion. SQL reads use the private index and leave the source index alone.
External writes remain invisible before refresh and appear after explicit Apply.
Dropping or changing the source index cannot break the committed copy; an explicit
refresh requesting a missing/filtered source index fails without replacing it.
Restoring the application index and DROP TABLE preserve all source objects.

The initial source-use counter assertion failed because FT.INFO itself increments
number_of_uses on this Software version. A direct owned probe confirmed successive
INFO counts 1, 2, 3 and one SEARCH followed by INFO counts 5, 6. The test now checks
the counter field through RESP3-aware reply parsing and calibrates the INFO cost
independently for both indexes. It requires no extra source use and positive SQL
use of the private index; it does not drop or skip that assertion.

A nine-case policy matrix combines automatic source selection, explicit source
index and direct HASH capture with default/selected/empty private policies.
Assertions inspect committed native metadata and actual FT.INFO attributes,
Boolean NUMERIC indexing, renamed fields, exact typed SQL values and unchanged
external text encoding. Foreign-copy coverage independently requires native
Boolean/number/TAG attributes, original key identity and source preservation.
Existing late-index failure rollback/source ownership checks remain active.

## Verification

Owned Redis Software fixture: active effective 4x QPF, Search, noeviction and strict
TLS reverified with make -C go redis-up. Database suites run sequentially.
- Before correction, the three-mode HASH regression failed: automatic and explicit
  source selection rejected IndexColumns; direct capture omitted the converted
  Boolean attribute. The separate foreign-copy regression also failed because
  its Boolean attribute was absent. Both failures were observed, not inferred.
- Initial corrected selected-column HASH matrix: three subcases passed, package
  time 2.440 seconds. A focused application-index/report/rollback run passed in
  3.995 seconds after correcting the counter-probe assumption.
- A subsequent related run passed in 6.290 seconds and the CLI race case passed
  in 2.097 seconds. Its anchored selector did not include the newly expanded
  policy cases; final policy-matrix evidence is recorded separately below.
- Final unit-only `go test -json -count=1 -p 1 ./...`: 904 pass events, 728 expected
  integration skips, zero failures. Skips do not prove compatibility.
- Final native library rebuilt with make -C go build and its version linker flag.

- Final expanded related race run: ten top-level cases, 23 total pass events,
  zero skips/failures, 7.844 seconds. The selector includes the nine policy
  combinations, foreign native attributes, source-key/encoding values,
  source selection/blocker reports and late-index rollback.
- Final CLI TestAdoptCommand race run: passed, no skips/failures, 2.079 seconds.

- Final full snapshot race rerun after the acquisition correction: 144 pass
  events, seven expected Cluster-only skips, zero failures, 130.608 seconds.
- Final rebuilt native C/ADBC snapshot validation: 13 passed, zero skips/failures,
  13.652 seconds.
- Changed Go formatting, local Markdown links and git diff --check passed.

The final source includes the acquisition correction committed as ca57e40.
IPC/Ray, full platforms, current Cluster recovery and comparative benchmarks were
not rerun for this checkpoint. This change removes index-policy restrictions and
restores eligible attributes; no throughput/latency parity claim is made without
comparative measurements.

## Admission failure found during validation

The first full snapshot race run was red: 140 pass events, seven Cluster-only
skips and one admission timeout classification failure, 136.426 seconds. A
three-repeat focused pass did not close it. A deterministic deadline/socket
regression reproduced I/O instead of Timeout in an internal store without the
client error hook. Acquisition now applies the existing transportContextError
normalization before wrapping the error; independent socket failures remain I/O.
Both cases require one attempt and no leaked pin. The original admission test
and the new two-case regression passed in 4.132 seconds. See the
[admission follow-up](admission-validation.md#deadline-classification-follow-up-2026-11-03).
The final complete rerun below includes that correction; no timeout was raised
and no assertion was weakened.

## Author review and remaining work

Reviewed capture membership, source decoding versus native storage, renamed and
empty index policies, internal attributes, index caps, report readiness, atomic
refresh failure and external ownership. Self-review is not maintainer approval.
Restricted ACL recipes, keyed-write/sweep compatibility, current large-operation
resumption, sustained compaction, latest Cluster/failover/recovery, platform matrix
and comparative benchmarks remain open. Historical results retain their source.
