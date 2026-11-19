# Specification

Date: 2026-11-18. Source: `818063a8`. Requestor: repository user. Author: Codex.
Status: implemented locally; no issue/PR created.

The input-only `__key` column defaults to the SQL expression
`<registered-prefix> || GEN_RANDOM_UUID()::TEXT` for unmanaged registrations.
The volatile function runs per row for omitted keys and DEFAULT, including
VALUES, implicit target lists and INSERT SELECT. Foreign registrations use
the primary prefix. Explicit NULL remains an error. Existing source-prefix,
collision, filtered-index, catalog-pin and no-replay checks remain authoritative.
RETURNING can expose the generated key; no internal key fields are stored.
Arrow ingestion continues to require explicit keys.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
