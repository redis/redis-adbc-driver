# Spec

Date: 2026-10-21. Base: `455acb7` (#418). Author: Codex; requestor: Albert Wong. Status: in progress. Issues #359, #360, #362, #363.

The user explicitly requires implementation of the runtime features in #359/#360/#362; design records alone do not complete those issues. The existing design selects retained final Arrow rows behind future executor-wide budgets, preserves live reads and rejects unsafe layouts/operators. Add direct regressions showing rejected planning/forged descriptors cannot mutate application HASH bytes, index definitions/ownership or legacy metadata. Require an abrupt planner exit and killed partially consumed worker to preserve live descriptor retry, with exact values/schema after updates/deletes and high-watermark exclusion. Require this native case in CI.

For #363 audit completed hosted artifacts, retaining exact revision, full-suite failures/skips, all 12 wheel client cells and TLS/physical-owner results. Link the existing repeated dense/sparse/skewed, narrow/wide partition/worker performance evidence with exact historical revision/limits. No full-scale performance or current-revision speedup claim. Do not close on workflow definitions or a known failing migration check.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
