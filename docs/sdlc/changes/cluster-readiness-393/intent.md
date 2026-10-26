# Cluster readiness after ALTER

Date: 2026-10-21. Source: merged #392, revision 6724c8a. Author: Codex;
requestor: Albert Wong (continue after merge). Status: implemented, review pending.
Issue: [#393](https://github.com/alberttwong/redis-adbc-driver/issues/393).

Concurrent ALTER regressed after #391: distributed FT.INFO can compare different
shard schemas during propagation and report either `Inconsistent index state`
(OSS Cluster) or `SEARCH_VALUE_BAD Inconsistent index state` (Software). Treating
these replies as terminal aborts a valid schema operation before metadata commit.

Recognize these exact server replies as unready, preserve cancellation and
metadata publication ordering, and fail closed for bounded partition readers.
Do not retry schema mutations, arbitrary errors, or broaden compatibility claims.
See [spec](spec.md) and [verification plan](plan.md).
