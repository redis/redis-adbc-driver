# Intent: configurable SQL and Arrow operation deadlines

Date: 2026-11-20. Source: `c6a75ca26682b3676161f55dcaa6f8e761f9edf0`.
Status: implemented and locally verified. Requestor: Albert Wong. Author: Codex.
Issue: [#546](https://github.com/redis/redis-adbc-driver/issues/546).
Related: [specification](spec.md), [plan and evidence](plan.md).

## Problem

A fixed 15-minute managed operation deadline aborts legitimate long Arrow
streams even when socket timeouts are longer. SQL statement_timeout is accepted
and displayed without enforcement, making the effective policy misleading.

## Desired outcome

Allow longer finite operation deadlines and explicit unlimited execution, with
consistent database, connection, SQL-session and statement controls. Apply the
same policy to all three data/index ownership modes, as clarified during work.
Preserve managed atomic publication, application-owned partial-write reporting,
cancellation, renewable snapshot leases, bounded writes and independent cleanup.

## Constraints and exclusions

No SQL/input replay, storage-format change, lease extension, benchmark rerun,
release or deployment. Native-reader, partition, shared-snapshot retention
and standalone maintenance lifetimes remain separate.

## Success and open decisions

Short fault-injected SQL/Arrow operations must time out with TIMEOUT/57014;
longer settings allow the same work. Earlier callers still win. Failed managed
append leaves committed rows unchanged, old readers valid and pins released.
Public documentation defines defaults, reset and scope/precedence. Maintainer
review remains outstanding; agent checks are not human approval.
