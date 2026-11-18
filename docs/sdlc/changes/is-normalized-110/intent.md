# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

Implement PostgreSQL 18 IS [NOT] [NFC|NFD|NFKC|NFKD] NORMALIZED and its is_normalized(text[,text]) function under #110. Reuse the merged Unicode 16 normalizer; preserve text/NULL/Arrow behavior. This does not close the wider #110 surface.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
