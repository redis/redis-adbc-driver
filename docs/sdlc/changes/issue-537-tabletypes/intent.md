# GetObjects table-type filtering: intent

Date: 2026-11-19. Source: `3e45955b`. Status: implemented, awaiting review.
Requestor: Albert Wong; author: Codex.
Issue: [#537](https://github.com/redis/redis-adbc-driver/issues/537).
See [specification](spec.md), [plan](plan.md), and [validation](validation.md).

## Problem and desired outcome

The upstream ADBC table-type filtering test is skipped because Redis's validation
configuration omits its capability declaration. Establish whether filtering is
implemented, then enable the declaration only with native driver-manager evidence.

Success means matching TABLE, VIEW and MATERIALIZED VIEW types, composing type
filters with existing discovery filters, and preserving restricted metadata access.
Changing ACLs, catalog semantics, constraint metadata or driverbase is outside scope.
