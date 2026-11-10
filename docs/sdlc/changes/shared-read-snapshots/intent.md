# Shared read snapshots: intent

Date: 2026-11-10. Source baseline: `85509485`.
Requestor: Albert Wong. Status: implemented and locally verified; awaiting PR review.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [specification](spec.md), [plan](plan.md), [validation](validation.md).

## Problem

Airbyte full-refresh reads multiple tables in separate SELECT statements.
Statement snapshots alone can return individually consistent tables from
different catalog generations, including inconsistent discovery schemas.

## Requested outcome

Acquire a single committed managed read view before discovery and retain it for
all metadata calls and sequential SQL reads on the owning native connection.
Preserve exact values and multiplicities despite concurrent supported writes,
schema/view changes, truncate/drop and reclamation. Fail explicitly after loss.

## Boundaries

Use existing protocol-17 reader pins with finite retention. The first API is
connection-local, read-only and excludes temporary objects and partition workers.
No process/connection attachment, write transaction, distributed-shard guarantee,
native upstream read primitive, Flight SQL session or connector release is added.
A released driver version and connector rollout remain release/integration work;
this PR must not claim those acceptance items are already delivered.
