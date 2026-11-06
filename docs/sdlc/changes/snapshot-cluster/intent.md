# Intent: Cluster API and recovery compatibility

Date: 2026-11-02. Parent: 2734c57. Requestor: user.
Status: implemented; validation recorded separately.
PR: https://github.com/redis/redis-adbc-driver/pull/284

Support Cluster API connections and verify multi-shard behavior, failover and
persistence recovery. The follow-up request includes remaining snapshot-specific
compatibility gates. Active-Active remains unsupported and noeviction required.
Earlier autocommit-only and no-migration decisions remain unchanged.

A catalog continues to use one tagged slot for atomic publication and fences;
Cluster API support does not imply distribution of one catalog across slots.
See [spec](spec.md) and [plan](plan.md).
