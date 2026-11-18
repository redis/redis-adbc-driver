# Intent: narrow unmanaged aggregate reads

Date: 2026-11-18. Requestor: Albert Wong. Author: Codex.
Source: `b06aea4f`. Status: implementation and validation in progress.

## Problem and consumers

[Q3 #493](https://github.com/redis/redis-adbc-driver/issues/493),
[Q4 #494](https://github.com/redis/redis-adbc-driver/issues/494),
[Q5 #495](https://github.com/redis/redis-adbc-driver/issues/495) and
[Q6 #496](https://github.com/redis/redis-adbc-driver/issues/496) exceed a
300-second wall deadline on 99,997,497 unmanaged application HASHes.
The source has 105 columns, while each query needs only one or two values.
The full unmanaged scan currently retrieves complete HASHes with LOAD *.

## Desired outcome and boundaries

Retrieve only needed live HASH fields for narrow full scans of wide unmanaged
tables. Keep exact SUM/AVG and bound single-column COUNT DISTINCT input state
with the existing external merge. Search continues to establish membership;
source HASHes remain authoritative and application-owned.

A fix is not established by selecting a faster plan alone: each original query
must completely fetch and match its independent full-data reference within the
unchanged deadline and memory contract before its performance issue is closed.
No reload, approximation, lease bypass, index mutation or deadline increase is
part of this change. Backup creation was requested and then cancelled before
any export or upload began. Existing S3 evidence is not a full dataset backup.

See [specification](spec.md) and [plan](plan.md).
