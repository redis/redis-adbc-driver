# Plan

Date: 2026-10-21. Base: `455acb7` (#418). Author: Codex; requestor: Albert Wong. Status: in progress. Issues #359, #360, #362, #363.

1. Audit each acceptance criterion and the completed hosted Release artifacts.
2. Add ownership/legacy and abrupt process-failure regressions, run native and race tests, and enforce native collection.
3. Reproduce and resolve the #418 post-migration failure without dropping exact checks or hiding failures.
4. Preserve source-scoped evidence/cleanup and open a reviewable PR. Continue with opt-in retained replay, global SQL, layouts and large IDs; keep the issues open until that runtime work meets its criteria.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Completed in this PR: ownership/legacy and abrupt process-death regressions,
stronger cursor-membership readiness, 12 TLS repetitions, full native validation,
race/cap checks, hosted artifact audit and owned-resource cleanup. Runtime
feature implementation continues separately; no issue is closed by this PR.
