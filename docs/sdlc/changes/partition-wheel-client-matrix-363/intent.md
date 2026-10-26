# Repeatable installed-wheel partition client matrix

Date: 2026-10-21. Source: 053e638 (merged #413). Requestor: Albert Wong (keep going); author: Codex. Status: implemented; eight local cells validated and fixtures cleaned up; hosted checks and human review pending. Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).

Reuse release-built wheels to make selected Python/ADBC TLS partition combinations durable on Linux x86_64/ARM64 and macOS ARM64. No new paid Cloud resources. Keep RESP3 as default. See [spec](spec.md) and [plan](plan.md).
