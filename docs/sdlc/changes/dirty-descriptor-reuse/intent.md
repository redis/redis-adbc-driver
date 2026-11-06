# Reuse the authorized dirty-table descriptor read

Date: 2026-11-06. Requestor: user. Issue [#336](https://github.com/redis/redis-adbc-driver/issues/336).
Baseline: `617e282ac2a06c57af9865799ec711ab670becc6` (merged PR #346).
Status: implementation and validation in progress.

The publication path reads a dirty table descriptor to validate its logical
identity, then `getTable` fetches and decodes that same private descriptor again.
Remove the second read within each enumeration while retaining all existing
hydration, compatibility, permission and snapshot proofs. Sealing and final
validation still load fresh descriptors separately. This targets two avoidable
GETs per dirty table/write; it adds no cache, format or mutation protocol.
See [spec](spec.md) and [plan](plan.md).
