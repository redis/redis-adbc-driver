# Multi-shard managed ingestion: intent

Date: 2026-11-20. Requestor: Albert T. Wong. Author: Codex.
Source: `1b5918e6f1e72ee6e5659efd72547e58108d21a4`.
Status: design assessment delivered; implementation and topology validation deferred.
Issue: [#568](https://github.com/redis/redis-adbc-driver/issues/568).

## Problem and outcome

Managed row HASHes, directories and publication all share `{meta}`. Four Arrow
callers can overlap conversion, transport and maintenance but do not distribute
those writes among physical command processors. Investigate one logical table
with independently placed physical partitions, without weakening statement
snapshots or the non-replaying input contract.

## Scope and exclusions

Deliver a concrete placement, ownership, publication, failure, read and backup
assessment with explicit blockers. Prefer a supported upstream facility before
building a driver distributed coordinator. This record does not implement a new
protocol, deploy a cluster, restart ingestion, or establish a speedup. It neither
promises linear scaling nor that every CPU/core can be saturated.

See [specification](spec.md), [design](design.md) and [plan](plan.md).
