# Specification

2026-10-22. Author: Codex. Base: `4e26896`. Status: implemented.
See [intent](intent.md), [plan](plan.md).

- Close signals and waits for active rekey/rewrite rollback before closing
  ordinary stream ownership and the maintenance client.
- Lease renewal and rollback use the maintenance pool independently of
  execution cancellation. WithoutCancel cleanup can use that pool after an
  execution interruption.
- Delete known Search cursor IDs through the maintenance pool on the original
  coordinator. Never replay a cursor READ or delete a cursor on another node.
  An unknown cursor ID after a lost first reply still relies on server expiry.
- Execution sockets contribute to the connection's active byte diagnostics.
- Child-context cancellation rejects late canceled dials but permits the live
  parent execution to redial. Caller/owner cancellation remains bounded and
  preserves CANCELLED/57014, no replay and unrelated pool usability.
- Trigger native Release PR checks for these lifecycle-sensitive source files.

Retain existing error, rollback, cursor absence and diagnostics assertions.
Keep credentials and operational evidence private. Use an isolated verified
Redis Software 4x-QPF fixture for development tests.
