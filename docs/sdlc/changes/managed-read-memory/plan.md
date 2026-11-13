# Plan

See [intent](intent.md) and [spec](spec.md).

1. Inspect raw reply/Arrow/prefetch lifetimes and reproduce retained page slots.
2. Release consumed outer page references while preserving reply/output ownership.
3. Verify completion, callback failure, cancellation and retained Arrow outputs.
4. Compare baseline/candidate native reads under an exclusive measurement slot
   on owned Redis Software with verified active/effective 4x QPF.
5. Run affected checks, review correctness/ownership/evidence, publish sanitized
   measurements and clean only task-owned resources.

Steps 1–3: initial unit/race and Software ownership checks pass. The baseline
regression fails on all six completion/error/cancel cases because consumed rows
remain referenced. Quantitative comparison remains pending.
