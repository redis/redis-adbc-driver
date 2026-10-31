# Intent: write adopted collections with explicit keys

Date: 2026-10-30. Source: `95d6583`. Requestor: repository user in this
Codex chat. Author: Codex. Status: implemented and verified locally.
[Issue #64](https://github.com/redis/redis-adbc-driver/issues/64),
[specification](spec.md), [plan](plan.md), [validation](validation.md).

## Problem

Application HASH collections adopted in place can be updated or deleted through
SQL, but cannot receive SQL or Arrow inserts. The driver cannot allocate keys
inside an application's namespace, and implicit wholesale deletion is unsafe.

## Desired outcome

ETL consumers can provide a new row's full `__key` to INSERT, INSERT SELECT,
MERGE INSERT and bulk append. Existing Redis keys must never be overwritten.
A statement option explicitly authorizes TRUNCATE of adopted row HASHes.

## Constraints and exclusions

Keep application ownership: no hidden HASH fields, sequence allocation,
re-keying RENAME, automatic key patterns or default permission to truncate.
Preserve the existing row boundary (no nested colon after the prefix) and text
encoding. Cross-key transactions and system columns in join conditions remain
outside this change. Human review has not been recorded.
