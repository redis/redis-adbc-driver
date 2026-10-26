# Intent

2026-10-21; baseline dc6f319; requestor alberttwong; author Codex.
Issue [#390](https://github.com/alberttwong/redis-adbc-driver/issues/390).
Status: implemented and locally validated; no maintainer approval.

Native macOS coverage for #363 exposed missing ordinary SELECT rows after indexed
ALTER operations. Preserve complete sequential post-ALTER reads without relaxing
assertions, granting SCAN, retrying partial query pages or disabling Search workers.
See [spec](spec.md), [plan](plan.md).
