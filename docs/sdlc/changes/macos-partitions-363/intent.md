# Intent

2026-10-21; baseline dc6f319; requestor alberttwong; author Codex.
Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).
Status: implemented and locally validated; no maintainer approval.

Replace load-only macOS wheel verification with required native Redis/Search
integration and full installed-wheel ADBC validation, including partition cases.
Reuse the existing release build and keep unsupported Cloud/macOS TLS claims
out of the result. See [spec](spec.md), [plan](plan.md).
