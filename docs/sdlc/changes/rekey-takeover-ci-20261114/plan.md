# Plan

[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Inspect latest Actions runs and the failed native macOS step.
2. Bound fixture ownership by stopping/joining only its automatic worker before
   staged output expires; retain both intentionally competing collectors.
3. Retain macOS workflow coverage for rekey integration test changes.
4. Run repeated focused tests with race detection on independently verified
   Redis Software4xQPF, validate CI selection/packaging helpers, and open a PR.

Implementation and local verification complete; remote CI/human review pending.
