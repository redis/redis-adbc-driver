# Contract and boundaries

Date: 2026-11-17. See [intent](intent.md) and [plan](plan.md).

Delete v0.0.1 key migration and its connection/adoption callers. Column lookup
always uses exact stored SQL spelling; the parser still folds unquoted SQL names.
Remove the FoldedIdentifiers field and its write sites. Old untagged metadata and
unsupported storage formats remain rejected without migration, deletion or source
HASH changes. Catalog-ID fallback and public ADBC ABI negotiation are separate.

Keep the current retained-source canonical identity component fixed to true,
matching current descriptors' former identifier marker. This preserves current
retained source tokens without retaining a metadata flag or lookup fallback.

Current compaction establishes an empty final index before copying. Remove the
older populated-output initial-scan path and old-binary handoff harness/tests.
A private job with copied rows but no required index is retired; it never installs
partial output. Keep all candidate entries until publication, rather than
consuming full-merge eligibility at claim, so a current worker can rebuild safely.
Keep leases, pending-batch repair, immutable source references, unknown-outcome
handling, current restarts, readiness and exact installation checks.

Rename HashColumn.InPlaceType to SourceType and JSON `in_place_type` to
`source_type`; update producers, CLI and tests. This intentionally breaks the old
Go/report field name. No deprecated alias is retained. The separately named
`sql_in_place` checklist identifier is outside this field rename.
