# Shared read snapshots: specification

Date: 2026-11-10. Source baseline: `85509485`. Status: implemented and locally verified.
Requestor: Albert Wong. Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Observable contract

The exact API and failure contract are in
[shared read snapshots](../../../shared-read-snapshots.md). Connection SetOption
`adbc.redis.read_snapshot=acquire` captures and binds one committed root before
metadata planning; `release` cancels active borrowed readers and removes the
parent pin. A diagnostic generation ID confers no attachment capability.

Retention defaults to one hour, configurable from one second to 24 hours while
unbound. Renewal retains the ordinary two-minute server lease, bounded by that
fixed deadline. Expiry/loss leaves a failed binding until explicit release.
Read failures/cancellation/child close do not implicitly unbind the parent.
Connection Close cleans up the pin. Process death expires normal server ownership.
The existing 128-pin and 16-generation bounds and writer backpressure apply.

## Implementation boundaries and risks

Connection/statement metadata, SQL execution, statistics and Substrait route
through a common read binder. Each child has fresh validation state and a local
cancellation link, borrowing one parent pin. GetObjects binds all callbacks to
one scope and materializes before release. The store's internal admission,
compactor and cleanup operations continue using their own ordinary pins.

Read-only admission rejects SQL/Arrow/retained writes, session/utility SQL,
sequence functions, foreign refresh, temporary objects and partition APIs.
Existing Redis authentication/native row and Search ACLs remain authoritative.
The RBAC regression checks grant revocation/restoration on the same historical
root. No persisted state or storage-protocol change is necessary.

## Acceptance checks

Compare complete metadata and table schemas, exact int64 values, NULLs and
duplicates while another connection publishes supported DML/DDL. Stream more
than one batch across update/truncate/drop; verify child close/cancellation and
parent release. Exercise expiry, lost server ownership, quotas and connection
Close. Kill a real process without cleanup and await two-minute lease collection.
Run unit, lifecycle/metadata and authenticated TLS Software 4x-QPF regressions.
Record skips and remaining release/integration evidence explicitly.
