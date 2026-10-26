# Intent

2026-10-21; baseline `85d6807`; requestor alberttwong; author Codex.
Issue [#377](https://github.com/alberttwong/redis-adbc-driver/issues/377).
Status: implemented and locally validated; awaiting maintainer review.

Prevent bounded Search readers from using an index whose initial backfill can
emit duplicate documents. Fail explicitly before exposing unsafe pages, retain
ordinary HASH fallbacks, preserve cursor cleanup and bounded memory. No snapshot,
unbounded deduplication set or claim of a fixed server version is added.
See [spec](spec.md) and [plan/evidence](plan.md).
