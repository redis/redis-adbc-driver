# Shared snapshot metadata discovery: specification

Date: 2026-11-10. Source baseline: `2c50655c`. Status: implemented and locally validated.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

GetObjects binds a single managed catalog scope before catalog/schema/table
filtering and passes it to every driverbase enumerator callback. It retains the
owned pin through synchronous metadata materialization and validates it before
returning an Arrow result. Errors release any constructed result and retire the
operation's local reference. A failed/expired shared binding must fail at every
discovery depth and for filters matching no catalog.

## Boundaries and risks

Preserve the API and lifecycle documented in
[shared read snapshots](../../../shared-read-snapshots.md), introduced by PR #369.
The wrapper delegates metadata formatting/filtering to driverbase; it changes
only snapshot ownership and validation. Ordinary GetObjects calls also obtain
one statement snapshot across all callbacks. Existing ACLs remain authoritative.
No persisted format, retention, attachment, partition, Substrait or Flight SQL
capability changes are introduced.
