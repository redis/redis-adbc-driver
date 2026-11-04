# Intent

Assess default enablement of bounded managed HASH prefetch at Albert Wong's
request, after PR #297 merged. Baseline: `4735cbd71a3fa89e68929daf258f5fb2216e1582`.
The prior prototype has a useful one-reader result, but final shutdown/main
integration changes were not measured and concurrent-reader results overlapped.

Measure identical final runtime sources with prefetch disabled/enabled on
verified Redis Software 4x QPF using restored data and independent full-value
validation. Keep narrow/GPU/unsupported-path fallbacks. Evaluate elapsed,
concurrency, memory and lifecycle before changing the normal-build selection.
No automatic promotion from historical results or allocation counts alone.

Use the original aggregate $300 cap and original eight-hour cleanup deadline;
previous run costs count toward that cap. Keep infrastructure/credentials private.
