# Current main integration and snapshot-aware prefetch

Date: 2026-11-03. Snapshot parent 644d9f4; incoming main 9822eb8.
Production remains protocol 15. Full [parity](plan.md) remains incomplete.

## Integration and behavior

GitHub reported the draft PR conflicting with main. A merge-tree preview and
the actual merge identified three conflicts: connection shutdown, scan execution
and transport cleanup. Incoming code includes Go 1.27.3, experimental scan
prefetch, SIMD experiments, CUDA execution and Arrow C Device interfaces. Its
historical benchmark/platform results do not validate this combined code.

The resolution retains immutable snapshot directory/candidate/ordered scans,
global late limits and the incoming CUDA/prefetch fields. Background worker
registration and shutdown share a lock; transport client closure joins workers
before closing their sockets/pool. SQL execution completion remains separate
from interruption: only its normal owner releases the execution registration
after private abort and pin cleanup. Connection Close retains its bounded wait
before closing maintenance resources. The incoming initial-fill worker test
models this registered execution and its normal completion explicitly.

Experimental prefetch also works on snapshot directory and indexed candidate
scans. The worker resolves newest versions/tombstones first, fetches winning
physical HASHes and sends owned reply slices. The consumer retains exact row-ID,
deletion-marker and cancellation validation before projection. Ordered/late,
residual and CUDA restrictions remain intact. No row/SQL replay, protocol change,
migration or additional ACL grant is introduced. Prefetch remains build-tagged.

## Focused verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and effective active 4x QPF verified before mutating suites. Tests run sequentially
on macOS ARM64; operational artifacts, raw logs and credentials remain private.
No ClickBench run/reload or elapsed-time performance claim.

An additive snapshot prefetch fixture validates all 64 columns of 599 visible
rows, including exact integers beyond 2^53, NULLs, replacements and tombstones.
Both authoritative-directory and selective indexed-candidate variants retain
the pinned values through UPDATE, DELETE, TRUNCATE, DROP and explicit GC, then
release their sources for reclamation. The initial indexed fixture used the
wildcard query that production candidate selection excludes; the final fixture
uses a numeric predicate and passes.

Final experimental selection (`-tags scanprefetch -race -count=1 -p 1`) exits
zero: 21 pass events, zero skips/failures; root package 86.315 seconds. It includes
both snapshot variants, worker cancellation/join, bounded Connection Close and
outcome diagnostics. The full 5000-row wide test passes actual experimental
selection assertions in public SQL streams and partition reads, validates exact
output/defaults/NULLs/aliases/ranges and checks a later publication cannot change
the pinned stream. The default build's wide test also passed in 75.13 seconds;
that preliminary selection failed only the subsequently corrected indexed test.

Final unit packages exit zero: 943 pass events, 829 expected integration skips.
Unit skips are not compatibility evidence. The versioned native library builds
with make -C go build. Native CPU C Device checks: three pass, two CUDA-only skips;
no CUDA host/kernel/device-interchange result is claimed for this integration.

The incoming live-HASH discovery/fetch test keeps all original update/delete and
all-NULL assertions on a separately owned unmanaged fixture, rather than corrupt
published managed HASHes. Its preexisting rows require an initial index scan;
the first fixture iteration incorrectly used SKIPINITIALSCAN and returned no
rows. The corrected race selection passes, zero skips/failures, root package
2.102 seconds. Snapshot behavior is covered by the separate additive fixture.
GPU experiment CPU correctness gates: six pass, one CUDA-only skip. CI matrix
helper tests: five pass. Merged workflow YAML retains required Ray trust and
native C Device gates; syntax, formatting and evidence-link checks pass.

## Remaining verification and review

Full combined snapshot race selection (`^TestSnapshot`, `-race -count=1 -p 1`)
exits zero: 235 pass events, 21 explicit skips, zero failures; root package
299.390 seconds. Skips are nine Cluster API cases, six permission cases and
six separately gated reader-model cases. They are not compatibility passes.
Rebuilt-native snapshot validation passes all 14 tests in 12.987 seconds,
zero skips/failures. Incoming CPU C Device binding passes both cases in
0.273 seconds, zero skips/failures. Strict Ray worker TLS compatibility passes
all 17 cases in 12.88 seconds, zero skips and three upstream warnings. These
database-mutating suites run sequentially after the Go selections; native build
uses the normal driver version linker flag and the fixture's verified CA.

No complete ordinary-suite rerun, hosted Linux matrix or CUDA validation is
claimed for the merged head. The prior ordinary diagnostic's 623 passes and
legacy putView failure are evidence for the earlier snapshot parent only.
Full ordinary fixtures, current Cluster API/failover/persistence, hosted/native
platform matrices, restricted reader ACL, explicit process resume, sustained
compaction and broader comparative benchmarks remain open. Author review covers
visibility order, reply ownership, worker joins, execution completion and pin
lifecycle; it is not independent maintainer approval.
