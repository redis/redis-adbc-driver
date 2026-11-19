# GetObjects table-type filtering: specification

Date: 2026-11-19. Source: `3e45955b`. Status: implemented, awaiting review.
Inputs: [intent](intent.md). Follow-up: [plan](plan.md), [validation](validation.md).
Issue: [#537](https://github.com/redis/redis-adbc-driver/issues/537).

## Functional requirements and implementation

Redis advertises TABLE, VIEW and MATERIALIZED VIEW. The native shim forwards the
requested list to driverbase; pinned driverbase revision `809dff5b62b1` applies
exact string membership after table enumeration. Multiple matching types form a
union; duplicate entries do not duplicate objects. Unknown, empty-string and
lowercase type names do not match advertised names.

An omitted list requests all types. Python driver-manager 1.13 converts both None
and [] to C NULL; both therefore request all types at that public boundary. The
Go API likewise treats nil and zero-length lists as unfiltered. A list containing
an empty string is an actual nonmatching filter, distinct from an empty list.

## Acceptance criteria

Enable `get_objects_tabletypes_filter` and run the named upstream test without a
skip. Additional native tests cover all three types, omitted/empty/multiple/
duplicate/unknown filters and intersections with catalog, schema, table-name and
column filters. Restricted-reader regression checks table-type filtering while
SELECT is denied and confirms metadata discovery makes no management ACL writes.

## Risks and unresolved concerns

This is a declaration and coverage change: no runtime fix is necessary with the
pinned driverbase implementation. Future driver-manager or driverbase changes
must preserve the tested public behavior. Real catalog support belongs to its own
issues; these tests use the existing default `redis` catalog.
