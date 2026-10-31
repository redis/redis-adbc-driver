# Intent

2026-10-30. Requestor: Albert Wong. Author: Codex. Base: `9124f0f`.
Status: implemented; focused/native validation passed, broad Go EOF limitation remains.

Work on [#210](https://github.com/redis/redis-adbc-driver/issues/210),
[#211](https://github.com/redis/redis-adbc-driver/issues/211), and
[#212](https://github.com/redis/redis-adbc-driver/issues/212) by closing remaining
metadata ACL, partition lifecycle, release-wheel and cancellation evidence gaps.
Existing statistics/live/retained implementations are already on current main.

A blocked cursor deletion must not hang cancellation after READ was interrupted,
even with disabled read timeouts. Restricted readers must get complete statistics
or explicit permission errors, and accurate declared constraint metadata.
Retained output must survive source changes without being mistaken for an atomic
source snapshot. Do not develop transactions, consistency storage or new
constraint enforcement. Do not infer support for untested release platforms.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
