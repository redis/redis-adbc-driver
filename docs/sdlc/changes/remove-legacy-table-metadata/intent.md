# Remove legacy table metadata and old-worker recovery

Date: 2026-11-17. Source baseline: `d14eaf4e`. Author: Codex.
Requestor: repository user. Status: implemented and validated; review pending.

The user requested removal of legacy table metadata, confirmed that deployed
workers should use current driver builds, and authorized renaming `in_place_type`.
Remove migration/identifier fallback code and older-compactor backfill support.
Rename the report field to `source_type` / Go `SourceType` without an alias.

Current-format restart/cancellation, published source data and retained readers
remain protected. Old driver storage remains explicitly rejected without writes.
See [specification](spec.md) and [plan/validation](plan.md).
