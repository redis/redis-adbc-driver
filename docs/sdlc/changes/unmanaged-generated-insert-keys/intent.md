# Intent

Date: 2026-11-18. Source: `818063a8`. Requestor: repository user. Author: Codex.
Status: implemented locally; no issue/PR created.

Allow unmanaged SQL INSERT to omit `__key` and reuse the existing SQL UUID
default. This matches the convenience already available for live HASH tables.
Application ownership and direct-write semantics remain in force. Arrow
ingestion, managed tables and index ownership are outside scope.

See [specification](spec.md), [plan](plan.md) and [validation](validation.md).
