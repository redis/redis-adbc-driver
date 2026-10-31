# Intent: bounded Substrait translation

Date: 2026-10-30. Source: `95d6583d2b07bdfdadc6ddf3b6368bf631139c24`.
Requestor: repository user, “work on 213.” Author: Codex.
Status: design/feasibility milestone; no maintainer approval recorded.
[Issue #213](https://github.com/redis/redis-adbc-driver/issues/213),
[spec](spec.md), [plan](plan.md), [evidence](validation.md).

## Problem and consumers

ADBC clients can submit SQL but cannot execute a Substrait plan. Preparing or
binding SQL and exporting the C setter do not establish plan support. Consumers
need an explicit compatibility boundary before a translator is introduced.

## Outcomes and scope

Assess a pinned read/filter/project subset against the current executor. Define
table/schema binding, function/type resolution, parameter behavior, admission
limits, state transitions and result/lifecycle validation. Make current support
status discoverable in public documentation.

## Exclusions and unresolved decisions

This milestone changes documentation only. A runtime translator, dependency
selection, enabling feature metadata and broad Substrait support remain future
implementation work. The proposed version/subset/budgets require maintainer
acceptance and a producer fixture before rollout. No live Redis validation or
performance claim is part of this design assessment.
