# Intent

2026-11-03; source `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`.
Requested by Albert Wong; author Codex. Status: implementation and design
recommendation complete; evidence and limitations recorded in validation.

Resolve [#193](https://github.com/redis/redis-adbc-driver/issues/193) and
[#195](https://github.com/redis/redis-adbc-driver/issues/195), close them after
acceptance, then create a PR in `redis/redis-adbc-driver`. Avoid merging the
unrelated historical worktree commits: this branch starts at current main.

For streamed wide managed-table reads, isolate bounded fetch/conversion overlap
from typed appenders, pipeline size and batch-size changes. For typed server
export, make the requested engineering recommendation and specify the contract
before adding any server/storage dependency. The audience is driver maintainers
and Redis server owners, not end users choosing a new supported option.

Do not claim speedups from helper tests, change authoritative HASH ownership,
add consistency/transaction features, silently require modules, or enable an
experimental path in release builds. Preserve the current RESP3 requirement;
the older issue's RESP2 criterion is superseded by current repository guidance.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
