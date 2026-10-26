# Plan

Date: 2026-10-20. Source: `11faf538`. Status: locally verified; PR CI pending.
[Intent](intent.md), [spec](spec.md), [evidence](validation.md).

1. Inspect failed release logs and successful matrix jobs; retain run links.
2. Replace seed-only Compose health with go/cluster-ready.sh, checking every
   primary and Search. Add deterministic readiness regression cases in scripts/.
3. Exercise the new gate and exact failed integration tests on an isolated
   cluster; inject an unavailable non-seed to check false-positive prevention.
4. Add read-only release diagnostics and run the readiness regressions in CI.
5. Open a fix PR; rerun the original failed job and inspect the independent
   published-release run. Keep failed attempts visible and do not move tags.
