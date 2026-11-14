# Direct Arrow input intent

November 13, 2026; requested by the user through the parent performance task.
Baseline `88774f10`. Status: implementation experiment, no adoption decision.
Reduce row-to-Arrow dispatch and copies without a second row staging block.
Preserve all SQL, error, ownership and snapshot contracts.
See [spec](spec.md) and [plan](plan.md).

Review: [PR #431](https://github.com/redis/redis-adbc-driver/pull/431) is open and unmerged; no human approval or release is claimed.
