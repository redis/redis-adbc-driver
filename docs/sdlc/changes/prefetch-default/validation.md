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

Seven Python validator/meter/RSS sampler tests passed. The new RSS test observes
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
