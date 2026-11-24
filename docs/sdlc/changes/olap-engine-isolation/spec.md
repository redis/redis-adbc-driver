# Specification

2026-11-23; see [intent](intent.md), [plan](plan.md). Setup and every engine/validator execute in separate sequential processes. Only the engine worker imports its database; Redis loads neither DuckDB nor reference rows. Exchange trusted typed files privately, validate through bounded disk sorting outside timing, preserve duplicates/numeric tolerance, and require validated complete results for ratios. Record engine versus validation failures and kill query children on supervisor cancellation/timeout. Reject historical/duplicate output reuse.

The third route is opt-in, uses the same Redis library/options via a read-only adbc_scanner catalog, disables whole-query remote pushdown and telemetry, records version/checksum provenance, and never silently substitutes another engine. The report covers only saved records and does not infer a full-suite score. No automatic database reset. Caller verifies Software and active effective 4× QPF.
