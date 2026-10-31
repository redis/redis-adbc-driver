# Intent: usable guides for every README example

Date: 2026-10-30 (America/Los_Angeles). Source base: `3fd6bf36eef8f6ab069a6ee06929e972a40c11f3`.
Requestor: repository maintainer in the Codex chat. Author: Codex.
PR: [#262](https://github.com/redis/redis-adbc-driver/pull/262). Status: local implementation and verification complete; human review pending; no human approval inferred.


The expanded README links many tools to validation reports without showing how
to use them. Every one of its 38 entries must instead lead to a concrete guide,
and every guide must be exercised. Keep the wider 50-tool ecosystem matrix
separate from the demonstrated local examples.

Scope: reuse existing guides, add missing ones, document setup and cleanup,
execute the guide instructions and publish per-entry results. Excludes new
connector implementations and untested remote/production compatibility claims.
See [specification](spec.md), [plan](plan.md) and [validation](validation.md).
