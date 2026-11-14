# Intent — November 14, 2026

Requested by Albert Wong's performance/PR follow-up; identified during coordinator
source review. Candidate implementation by the managed-publication Codex subagent.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).
Parent context: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

A joined/wrapped native NOPERM plus an unknown transport outcome can currently
select write-only index reconstruction after an uncertain query proof. Composed
readiness failures can also be mislabeled Unauthorized or classified transient.
Require direct native proof before compatibility reconstruction or polling. Retain
native write-only compatibility and exact original diagnostics for unknown errors.

This safety-only change deliberately omits the unselected publication-readiness
pipeline experiment. No latency improvement is claimed for this correction.
