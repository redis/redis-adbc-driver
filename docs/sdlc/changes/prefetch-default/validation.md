# Validation in progress

Initial runtime baseline: merged main `4735cbd71a3fa89e68929daf258f5fb2216e1582`.
Matched off/on builds use Go 1.27.3. The Go runtime source is identical; only the
existing compile-time prefetch selection differs. Tooling is tracked separately.

The full enabled Software race suite passed on an isolated authenticated TLS,
RESP3, three-shard Redis Software 8.6.2 fixture with active verified 4x QPF:
`make -C go test-software TEST_ARGS='-race -tags scanprefetch'` (isolated project
and ports). Driver package: 420.762 seconds; five tested packages passed, with
the additional native CUDA stub package having no test files. Across packages,
835 top-level tests passed and 41 existing tests skipped. Skips cover Software
proxy ACL/protocol introspection limits, opt-in profiles and unavailable native
GPU/extra endpoints; no assertion/skip changes were made.

Both matched C libraries passed regular/public partition and one/four-reader
checks against 5,000 × 105 independently generated typed rows, including all-NULL
rows, embedded NUL strings and integers above 2^53. All output values, physical
types, validity and multiplicity were compared outside fetching. This is a
correctness smoke check, not a ClickBench performance result.

Eight Python validator/meter/RSS/summary tests passed. The new RSS test observes
an actual child allocation; it does not infer RSS from the parent's high-water.
The temporary Arrow environment initially lacked psutil; installing the standard
dependency resolved that setup failure. The sampler requires psutil explicitly.

The new restore pilot passed a fresh 20-row × 105-column independent official
Parquet membership/value check before fleet expansion. All 49 task-owned
instances passed ownership/protection, tagged root-disk deletion and cleanup
permission verification. A transient first Lambda verification observed stale
configuration; the exact two-instance configuration was verified before
continuing, without provisioning duplicates. The original aggregate $300 cap
and original eight-hour deadline are preserved across both runs.

Performance matrices, final default disposition and task cleanup are pending.
No default-enable or final performance claim is made on these checks alone.

Additional audit reproduced a dropped-table error being lost when producer EOF
preceded final-page consumer conversion. The failing test returned EOF; adding
consumer EOF readiness preserved the existing stale-read StatusIO. A separate
failing test showed an unnecessary worker for a known single-page discovery;
that path now stays synchronous. Both regressions and the focused wide/prefetch
race suite pass (15.189 s driver package). The full enabled suite is being
repeated because these are new runtime changes. Timings have not started: both
matched remote trees will be rebuilt with these refinements before measurement.

The corrected-source full enabled Software race suite passed again (driver
417.459 s; 837 top-level passes, 41 unchanged skips). Runtime revision is
`158b8c820c476a7ae116fb677c6f1fffa4722b98`. The matched measurement build trees are identical
and include both refinements. Timings are gated on this completed pass.

The C-boundary summary retains unsupported Go heap/allocation counters as null,
not zero, and rejects mixed missing instrumentation. The eight Python tests
include that reporting contract. The C and constrained-CPU matrices were both
extended to twenty pairs before any timings began.

The completed twenty-pair C shared-library screen passed 336 executions. Wide
median elapsed improved 14.03% regular / 14.35% partition with one reader, and
5.52% regular / 8.70% partition with four readers. One-reader cases won 20/20
pairs and four-reader cases won 18/20. All wide paired-bootstrap upper bounds
were below zero. These are completed C results, not yet the overall promotion
decision; the Go/64-column/constrained-CPU screens remain pending.

Wide C sampled process-RSS medians increased about 17–24 MB. Narrow controls also
show a process-RSS offset, so do not call the whole RSS difference the cost of an
owned lookahead page. A live narrow-projection assertion in the existing wide
API test confirms fewer than 64 required columns and no prefetch worker; the
focused Software race test passed (14.255 s). The theoretical bound remains one
additional reply page per active eligible reader; variable-width values matter.

The completed Go 105-column screen passed 336 executions and its declared gate:
one-reader medians improved 14.10% regular / 14.96% partition, with 20/20 wins.
Four-reader medians improved 1.10% regular / 6.57% partition. The regular
four-reader paired interval includes zero and has an upper endpoint +2.06%,
within the predeclared +5% nonregression margin; do not claim a consistent gain.
Narrow medians were near unchanged. Go current-RSS medians were close between
builds; this does not eliminate the theoretical additional owned page.

The completed 64-column screen also passed 336 executions and the gate:
one-reader medians improved 12.28% regular / 13.10% partition, all 20 pairs won.
Four-reader medians improved 6.20% regular / 3.10% partition; regular uncertainty
includes zero, but all wide upper endpoints are within +5%. The constrained-CPU
screen remains pending. A default-enabled candidate is now under normal-build
validation; final promotion remains conditional on the last elapsed gate.

Main's #298 merge adds an isolated SIMD prototype/profiling tests, with no
production SQL/read-path changes. It is integrated for final build/CI checks;
measured production scan code remains pinned to corrected runtime `158b8c8`.
The shared local AWS Python environment lost boto3 during monitoring; a fresh
task-owned control/validation environment restored it. Remote trials continued
unaffected. It uses the runner's pinned PyArrow/ADBC dependencies; eight Python
checks pass there too. SSH tooling is now task-owned as well.

The normal default-enabled build (no experimental tag) passed the complete
Software race suite after main integration: driver 356.591 s, 840 top-level
passes and 43 unchanged-capability/opt-in skips. All six tested packages passed;
the CUDA stub package has no test files. The explicit opt-out build is also
being checked. Final normal-build full-fetch confirmation is queued behind the
constrained-CPU result; it will use these selectors and both Go/C consumers.
