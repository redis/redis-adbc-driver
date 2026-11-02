# Statement snapshots for managed tables: intent

Date: 2026-11-02. Source revision: `bb83527f305b75ea731696f55f78eb75121bbbc0`.
Requestor: Albert Wong. Status: mandatory snapshot runtime implemented in the working tree.
Issue: [redis/redis-adbc-driver#113](https://github.com/redis/redis-adbc-driver/issues/113).
Related records: [specification](spec.md), [implementation plan](plan.md),
[validation](validation.md).

## Problem

One SELECT can currently combine different versions of a table: Search cursor
pages and subsequent HASH reads run while writers change rows in place. Joins,
subqueries and independently pushed aggregates can also observe different data.
Completing a result in memory does not establish that its inputs were a snapshot.

## Requested outcome

Start with statement-level snapshots for driver-managed tables. Every supported
SELECT must read one committed catalog root, including the table schemas and
immutable row generations referenced by that root, for its complete execution
and Arrow stream lifetime. A query started before a publication reads the old
generations; one started afterward reads the new ones.

The user's 2026-11-02 request supersedes the issue's earlier scope decision for
this feature. It does not authorize treating transactions, rollback or savepoints
as implemented.

## Boundaries

Snapshots are automatic for every managed-table connection and cannot be
disabled. The user explicitly excluded an upgrade path. Connections initialize
fresh empty catalogs; existing unversioned catalogs fail without modification.
An isolated database with ACL-restricted writers is the deployment boundary.

Snapshot mode accepts SELECT over permanent managed tables and views
whose full dependency closure is managed. Adopted HASH tables, application
indexes, foreign indexes, temporary tables and mixed managed/external queries
must fail explicitly in this initial format. A public non-snapshot managed-table
mode is not available.

Although the public read guarantee is limited to SELECT, all supported writers
in a snapshot catalog must publish immutable generations. INSERT, UPDATE,
DELETE, MERGE and Arrow ingestion are therefore prerequisites, not optional
follow-up fixes. Unsupported DDL and maintenance operations must be rejected
before changing snapshot storage until they implement the same protocol.

## Non-goals

Multi-statement transactions, ADBC autocommit=false, rollback, savepoints,
serializable isolation, Active-Active multi-writer publication, external HASH
snapshots, live migration with old writers, and SQL access to historical roots.

## Success criteria

The deterministic concurrency and failure cases in [validation.md](validation.md)
pass against supported Redis Software configurations with verified 4x QPF.
No path reads current metadata or mutable rows after capturing a root, and no
cleanup path can reclaim a generation belonging to a valid reader.
