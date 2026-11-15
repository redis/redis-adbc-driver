# Intent

Date: 2026-11-14. Requestor: Albert Wong ("work on performance tasks as subagents").
Author: Codex unmanaged-query subagent. Source baseline: `b904cf8d` (PR #439).
Status: evidence/harness delivered; production experiment rejected after measured
four-client SQL latency loss. Human approval is not inferred.

Investigate avoidable client allocation in exact unmanaged source retrieval and
expose its previously unmeasured sparse/all-NULL/wide costs. Preserve source
semantics and retain negative evidence. No runtime changes are selected. The
existing dense filtered aggregate still makes one native source read per matched
HASH; grouped/full scans retain their existing bulk LOAD* path.

The field-boxing source, ownership regression, patch and all observations are
archived. New opt-in profiling and audit tools remain for follow-up. Scope excludes
managed admission/point paths, approximate reducers, ClickBench and new upstream
issues. [Specification](spec.md), [plan](plan.md), [validation](validation.md),
[source API analysis](source-contract.md). Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
