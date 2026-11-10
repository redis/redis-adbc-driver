# PostgreSQL 18 mismatch fixes: intent

Date: 2026-11-09. Author: Codex. Requestor: Albert Wong. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9` plus PostgreSQL 18 target changes. Status: in progress. Issue: [#359](https://github.com/redis/redis-adbc-driver/issues/359).

Resolve the 188 retained PostgreSQL 18 SQL/ADBC mismatches and open a PR. Preserve the 11 transaction/locking/relational-integrity cases in #360 and the full-corpus accounting. No passing claim may be based on new skips or widened allowlists.

See [spec](spec.md) and [plan](plan.md).
