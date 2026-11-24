# Intent

Date: 2026-11-24. Requestor: Albert Wong, through [issue #609](https://github.com/redis/redis-adbc-driver/issues/609).
Source: `f6e0bbbe`; local verification and regression follow-up. See
[specification](spec.md) and [plan](plan.md).

Reject uncertain HASH pipeline outcomes even when every individual HMGET reply
contains valid values. Preserve the established tolerance for direct `redis.Nil`.
The production correction and parsed scalar-cohort regression already arrived
through merged [PR #610](https://github.com/redis/redis-adbc-driver/pull/610)
while this task was running. Add reader-level coverage without changing that fix.
Other read helpers, benchmarks and deployment are outside scope. The follow-up
PR will close issue #609 on merge, as requested by the user.
