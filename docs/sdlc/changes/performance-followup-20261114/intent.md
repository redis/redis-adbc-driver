# Intent

November 14, 2026. Requested by Albert Wong; implemented and cross-reviewed by Codex subagents. [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Improve remaining point-read overhead and the four-client unmanaged filtered
aggregate regression, while fixing the PR's cleanup-test failure. Investigate
managed publication without presenting mixed or slower measurements as a win.
Keep source ownership, exact SQL values, leases and unknown-outcome rules.

[Spec](spec.md), [plan](plan.md), [validation](validation.md).
