# Intent

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439). Date: November 14, 2026. Requested by Albert Wong; implemented by Codex.

The amd64 Software driver CI job failed TestSQLAlterCleanupResumes waiting five
seconds for a reopened background worker to reclaim an interrupted ALTER output.
Keep the original recovery and source/current ownership assertions while removing
the test's dependency on the worker's one-second idle scheduling cadence.
Production behavior, public options and historical performance evidence are unchanged.

[Spec](spec.md), [plan](plan.md), [validation](validation.md).
