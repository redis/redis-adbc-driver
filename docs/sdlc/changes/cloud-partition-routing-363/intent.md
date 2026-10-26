# Cloud Pro sharded partition validation

Date: 2026-10-21. Source: merged #407. Author: Codex; requestor: Albert Wong (keep going; Redis Cloud Pro by default). Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).

Status: implemented, validated and cleaned up; human review pending. Prove the existing native partition path against a disposable multi-shard Cloud Pro deployment, using independent restricted workers and verified TLS. Extend platform evidence with an installed Linux ARM64 wheel. See [spec](spec.md) and [plan](plan.md).

User follow-up: retain RESP3 as the default. Verify both client defaults and preserve RESP2 only as explicit compatibility coverage.
