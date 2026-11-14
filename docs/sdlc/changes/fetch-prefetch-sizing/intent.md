# Fetch/prefetch sizing experiment

Requested November 13, 2026; baseline `88774f10`. Test whether reducing the
eligible single-source fetch/page ceiling from 4096 to 2048 rows lowers
peak worker RSS without material end-to-end regression. Existing prefetch still
owns at most one additional page. No deeper queue or adaptive production policy.

See [spec](spec.md) and [plan](plan.md). Publication is subject to agent review
and measured acceptance; no human approval or speedup is claimed yet.

Review: [PR #432](https://github.com/redis/redis-adbc-driver/pull/432) is open and unmerged; no human approval is claimed.
