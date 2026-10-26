# Independent Redis Cloud Pro partition credentials

Date: 2026-10-21. Source: 1bb46ca (merged #405). Author: Codex; requestor:
Albert Wong (continue work; default to Redis Cloud Pro). Status: implemented, validated and cleaned up; human review pending. Refs [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363).
See [spec](spec.md) and [plan](plan.md).

Close the Cloud independent-credential gap with a real disposable Pro database.
Reuse the exact partition value/schema, denial, cancellation, release and stale
checks already used against OSS/Software. Keep API keys out of worker processes.
Do not touch shared databases or existing account ACL resources.
