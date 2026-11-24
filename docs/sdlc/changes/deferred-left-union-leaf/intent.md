# Intent: preserve LEFT-leaf demand in a deferred UNION

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`.
Status: local candidate, unreleased. Requestor: repository user through parent
DS investigation; author: Codex.

TPC-DS Q5's web returns UNION branch has a compound-key LEFT join to web sales.
The prior safe UNION gate eagerly materializes that whole source. Admit only a
pure direct-source LEFT projection, allowing parent date demand to narrow the
preserved return input before joining. Preserve compound-key duplicate/NULL
semantics, nullable site values, complete residuals, and cached-plan isolation.
Do not broaden arbitrary outer joins or claim fully streamed LEFT output.
Origin: isolated committed change `20f20e67bca46dcb11b946ee6d3c38c1bc9d181c`,
applied without its integration ancestors. See [specification](spec.md),
[plan/evidence](plan.md). Related: [TPC-DS Q5 #591](https://github.com/redis/redis-adbc-driver/issues/591).
