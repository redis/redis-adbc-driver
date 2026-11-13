# Plan

See [intent](intent.md) and [spec](spec.md).

1. Inspect raw reply/Arrow/prefetch lifetimes and reproduce retained page slots.
2. Release consumed outer page references while preserving reply/output ownership.
3. Verify completion, callback failure, cancellation and retained Arrow outputs.
4. Compare baseline/candidate native reads under an exclusive measurement slot
   on owned Redis Software with verified active/effective 4x QPF.
5. Run affected checks, review correctness/ownership/evidence, publish sanitized
   measurements and clean only task-owned resources.

Steps 1–4 complete: unit/race and Software ownership checks pass. The baseline
regression fails on all six completion/error/cancel cases because consumed rows
remain referenced. All 56 native comparison trials passed full output validation.
Four-reader median peak worker RSS fell about 3.3%; elapsed increased 0.5–3.7%
in this baseline-first run. See [validation](validation.md) for distributions,
limitations and the pending human acceptance decision. Final adjacent checks
and owned-fixture cleanup remain before handoff.

Additive follow-up: inspect per-row HMGET field boxing, prebox immutable field
names once per batch, then verify full command metadata, independent args and
results, raw RESP bytes/replies and pipeline errors. Run a focused allocation
comparison with the module-selected toolchain before requesting another native
comparison slot. Preserve the clear-only comparison unchanged and label the
combined candidate separately.
