# Intent

2026-10-21; baseline `95d6365` (merged #373). Requestor: alberttwong.
Issue: [#374](https://github.com/alberttwong/redis-adbc-driver/issues/374).
Status: implemented and locally validated; awaiting maintainer review.

Keep ordinary ExecuteQuery and portable partition execution as separate public
APIs while sharing cursor/Arrow stream ownership and safe bounded numeric scans.
Replace whole-result HASH fallback only where exact coverage and existing
ordinary ordering/limit semantics are preserved. Retain broad SQL and other
layout fallbacks. No public API deprecation, new worker scheduler or descriptor
serialization inside ExecuteQuery.

See [spec](spec.md), [plan and evidence](plan.md).
