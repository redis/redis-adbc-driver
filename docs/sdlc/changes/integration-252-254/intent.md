# Intent: candidate integration follow-ups

Date: 2026-10-30. Requestor: Albert Wong. Source: `91064e6`.
Status: local implementation and verification; upstream reports submitted; dependency fixes await maintainer resolution.
Issues: [#252](https://github.com/redis/redis-adbc-driver/issues/252),
[#253](https://github.com/redis/redis-adbc-driver/issues/253),
[#254](https://github.com/redis/redis-adbc-driver/issues/254).
Next: [specification](spec.md), [plan](plan.md), [validation](validation.md).

## Problem

adbi rejects valid unnamed parameter metadata, Datasets cannot reopen an empty
default-sharded save, and the native candidate launcher lacks a real Ray TLS
fixture. These prevent completing the three integration acceptance records.

## Desired outcome

Retain precise dependency regressions and prepare upstream fixes without changing
the driver's ADBC contract. Exercise Ray worker certificate trust against an
owned Software TLS endpoint, with complete rows/schema and automatic cleanup.

## Constraints and exclusions

Use verified active 4x QPF; keep credentials/keys private and trust scoped to the
Redis connection. No Flight SQL, driver parameter renaming, SQL interpolation,
global CA installation, upstream release claim, or broad sweep claim.
