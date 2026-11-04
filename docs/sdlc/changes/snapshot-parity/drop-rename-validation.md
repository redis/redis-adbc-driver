# DROP/RENAME publication, name reuse and reclamation

Date: 2026-11-03. Parent ea20f38. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and restored semantics

The previous ordinary diagnostic has 491 pass events, ten capability skips and
two failure events (one child and its parent), root package 179.049 seconds. It
stops at TestSQLRenameDuringDrop/before_the_index_is_dropped/plain: DROP receives
40001 after RENAME publishes. The old fixture expects DROP to follow the rename
and delete the relation under its new name. That behavior would violate the
statement's pinned metadata dependency.

The restored fixtures require a precise serialization refusal from a stale
DROP or RENAME. They exercise the actual public statement hooks after descriptor
read and before private metadata removal. These phases do not remove a published
index. No sleeps, automatic SQL replay or mutable-move APIs are used.

- Six plain-rename cases cover both DROP boundaries, ordinary/IF EXISTS DROP and
  creation of a replacement under the old name. After refusal, the renamed
  relation retains its exact rows and physical identity. A replacement retains
  its own values and row-ID sequence. Only a fresh explicitly named DROP retires
  the renamed relation; GC and subsequent name reuse are checked.
- DROP publishing during RENAME forces the stale rename to return 40001. GC
  must keep its source index while the operation is pinned, then reclaim the
  retired source after that operation exits. Both catalog names become reusable.
- Two forced-rekey cases publish at the same DROP boundaries. The old index and
  both rows remain protected through explicit GC while DROP is in flight. The
  stale DROP conflicts; the new UUID layout retains exact rows and the old one
  reclaims afterward. A separate post-publication case retains exact old rows
  through fresh DROP/IF EXISTS of the absent old name; the renamed table survives.
  Its pinned old row-ID sequence remains readable even while that name and
  sequence are absent from the current manifest.
- An owning connection's temporary rename survives its stale DROP. Current
  metadata exposes exactly the renamed object, then explicit DROP leaves neither
  descriptor nor sequence and normal GC reclaims its physical rows/index.

Absence assertions now inspect descriptor and sequence membership under one
current-manifest pin. Old pinned generations may still retain physical metadata;
the legacy mutable name registry cannot establish current snapshot absence.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites run sequentially on macOS ARM64. No ClickBench run/reload. Operational
artifacts, credentials and infrastructure identifiers remain private.

Focused DROP/RENAME/rekey/temporary selection with `-race -count=1 -p 1` exits
zero: 13 pass events (four top-level tests and nine nested cases), zero
skips/failures; root package 11.120 seconds. Production code and storage are
unchanged.

After adding the explicit old-sequence retention assertion, the final focused
selection again exits zero: 13 pass events, zero skips/failures; 11.136 seconds
for the root package.

Final unit packages exit zero: 923 pass events, 823 expected integration skips
and zero failures. These skips do not establish compatibility. Go formatting,
local evidence links and diff whitespace checks pass.

The subsequent ordinary diagnostic with `-json -failfast -count=1 -p 1 -skip
TestACLAppIndexReader` passes the restored races and reaches 563 pass events,
13 explicit skips and one failure; root package 290.465 seconds. It stops at
TestSQLForeignIndexName's unscoped legacy lastNames setup; later cases did not
run. Skips include profiling gates and Software administration limitations,
rather than thirteen proven unsupported SQL features. The ACL exclusion remains
command-line-only. No repository skip is added and full ordinary success is not
claimed.

Hosted Test run [41788774439](https://github.com/redis/redis-adbc-driver/actions/runs/41788774439)
tests the earlier ea20f38 head. Helpers and coverage succeeded; both Linux
Software jobs reached Go tests. AMD64 subsequently failed at Go tests while
ARM64 remains running at inspection. The CLI withholds full logs until the run
is terminal, so no uninspected primary failure or skip count is inferred.
That run does not test these restored fixtures or establish complete platform
parity. Step completion is not treated as a count of unskipped native cases.

## Author review and remaining work

Reviewed dependency conflicts, exact error states, replacement identity, temporary
ownership, pin-protected indexes/data, current-manifest absence, deterministic
hook execution and reference-aware reclamation. This is not independent
maintainer approval. Full ordinary/current Cluster/platform/recovery, restricted
reader ACL integration, process resume, sustained compaction and broader
performance comparisons remain required. Protocol 16 is not activated.
