# Validation

[Intent](intent.md), [specification](spec.md), [plan](plan.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

At initial implementation, focused `TestUnmanagedProjectionRead` unit selection
passed with REDIS_URI/REDIS_SNAPSHOT_URI unset, GOMAXPROCS=4, Go 1.27.3,
`-count=1 -p 1`. Native compatibility and performance are not established by this
unit-only result. Actual Software attribution and SQL measurements are pending.
No archived evidence or old source identity has been changed.
