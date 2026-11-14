# Intent — November 14, 2026

Requested by Albert Wong, implemented by a Codex subagent. Human review pending.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

The guarded selected-field path improves one-client filtered aggregation but the
archived four-client case is 38.1% slower and has 23.0% less finite throughput.
Attribute the regression before claiming a cause. Reduce avoidable request or
server work while retaining live HASH values, exact native bytes, value/request
bounds, per-statement permissions and fail-closed unknown replies.
