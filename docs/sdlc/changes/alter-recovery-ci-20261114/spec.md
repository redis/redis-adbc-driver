# Contract

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439). Date: November 14, 2026. Requested by Albert Wong; implemented by Codex.

Resolve the actual reopened connection's store through a short-lived statement.
While the existing five-second wait observes the old row directory, coalesce a
normal maintenance wake when work remains. Only the background worker performs
recovery; the test must not reclaim inline, replay SQL, lengthen the deadline,
relax row/ownership assertions or add skips. Keep the acknowledged first256-row
batch, remaining344members, pinned600-row source and exact current output checks.

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
