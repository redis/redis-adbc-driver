# Validation status

[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Source inspection and gofmt/diff checks pass. Focused no-URI race selection:
4 top-level PASS / 1 SKIP (native Software integration intentionally absent) /
zero failures. Focused verified Redis Software race selection:
18 top-level PASS / zero skips/failures. Checks include exact queued native
commands and NoRetry, whole-pipeline unknown/composed errors, direct native
transient handling, private-output pipeline/value proof, index readiness
terminal errors, TRUNCATE readiness, notification ownership/byte/row bounds,
permission compatibility, cancellation/lost replies, snapshot conflict fences
and published-source validation. Unit skips do not prove live compatibility;
the native pipeline test passed in the separate Software selection.

The fixture's active/effective 4× QPF, six workers/mnp32, noeviction and strict
TLS/RESP3 were independently gated before each command by the coordinator.
Go packages were sequential with caching disabled. Raw logs remain private.
These runs were made before the following frozen candidate commit with the
same tracked production/test contents; final commit identity is recorded in
the diagnostic provenance rather than relabeling older archived benchmarks.

Matched diagnostic timings and independent final review pending. No latency
speedup claimed yet. The candidate removes one serial exchange from an ordinary
first-ready output; it keeps both native reads and all later publication proofs.

## Frozen diagnostic and subsequent safety correction

Frozen b5e71b05 was profiled against baseline 7b94a93c with precompiled binaries
in A-B-B-A order. All 384 mutation observations and complete post-state checks
passed. The [complete diagnostic](../../../../benchmarks/managed-publication-readiness/2026-11-14/report.md)
retains every result and exact native proof commands. One client exchange was
removed from every private output, but timings were mixed: pooled INSERT/UPDATE
p50s were 8.9%/8.6% slower, DELETE p50 5.2% lower and all p95s higher. No general
latency benefit is established. The uninstrumented native subset is pending.

After that freeze, parent review identified existing composed-NOPERM gaps in
validateSnapshotTables. New real-validator tests reproduced joined/wrapped proof
errors triggering replacement CREATE and composed later FT.INFO errors being
mislabeled Unauthorized. A future/custom pipeline local queue error was also
reproduced as ignored by the new helper. Three top-level regression groups failed
on the predecessor. An initial guarded patch assertion left source unchanged and
a second RED run is retained in checks despite its preliminary log name. Direct
native assertions and checked Process errors now pass the focused unit race run:
7 top-level PASS, 3 explicit missing-native-fixture SKIP, zero FAIL. The skipped
native compatibility checks require the next exclusive Software slot.

These safety changes are later source and are not relabeled as b5 timings.
Independent point-agent source review found no actionable issue in the initial
selected-primary batch; final safety follow-up review remains pending.

## Selection outcome

**NOT SELECTED for integration.** The [native subset](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md)
retains all 96 records, mixed gains and material DELETE latency regressions.
The coordinator chose to omit publication batching and retain only direct-native
fail-closed safety fixes with real-validator regressions. Frozen experimental
source/timing identities and the code patch remain archived; no performance
results are relabeled as the later safety-only source.
