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
limitations and the pending human acceptance decision.

Additive follow-up: inspect per-row HMGET field boxing, prebox immutable field
names once per batch, then verify full command metadata, independent args and
results, raw RESP bytes/replies and pipeline errors. Run a focused allocation
comparison with the module-selected toolchain before requesting another native
comparison slot. Preserve the clear-only comparison unchanged and label the
combined candidate separately.

Additive follow-up complete: constructor allocation comparison passed, focused
race tests and 29 live Software test pass events have no failures/skips. Combined
candidate native build/smoke and all 28 additional trials passed full validation.
All 84 native trial samples and all ten construction samples are retained.
Owned Software container/network and generated endpoint env were removed;
other containers/networks remained present. Human review/PR coordination is
with the parent task; no PR or issue was created by this agent.

Final unit-only all-package pass completed on combined tree `c3d0f0b8`: six
packages passed, three had no tests, zero failures. Expected integration/profile
skips are recorded in validation; no additional server or timing run was needed.
