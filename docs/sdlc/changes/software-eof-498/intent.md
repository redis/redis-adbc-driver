# Intent

2026-10-22. Author: Codex; requestor: Albert Wong. Base: `590ab0a`.
Status: investigation; root cause unresolved.

Investigate [#498](https://github.com/alberttwong/redis-adbc-driver/issues/498):
intermittent EOFs during metadata reads and connection initialization on local
macOS Docker three-shard Redis Software. Determine whether the same tests fail
when the client bypasses host forwarding. Preserve errors and complete Arrow
result validation; do not add retries, skip failing assertions or develop
transaction/consistency features.

See [spec](spec.md), [plan](plan.md), [evidence](evidence.md).
