# Specification

See [intent](intent.md) and [plan](plan.md). Date: 2026-10-22.
Source: `f9045da`. Author: Codex. Status: implemented.

The real ACL statistics test must require I/O status and the preserved
`FT.SEARCH failed: NOPERM` reason after revoking FT.SEARCH on every primary.
It must require a nil result reader. A successful zero count, unrelated error,
or partial reader must fail. Keep the preceding permitted statistics request.
Do not change production error mapping or introduce new skips.

Redis Software remains the default fixture with verified active 4x QPF.
Because Software blocks ACL SETUSER, use the existing OSS Cluster compatibility
fixture to exercise this real command denial. No benchmark is required.

For the follow-up, worker diagnostics may contain only numeric exit status and
fixed allowlisted categories. Never print stderr, credentials, SQL or partition
payloads. Keep post-resharding exact Arrow values/schema and physical ownership
checks. Investigate incomplete reads after the fixture's convergence gate.

The owned resharding fixture must prove readiness through both all-primary
administrative aggregates and fresh restricted native workers, for three
consecutive complete rounds within the existing 30-second gate. During this
gate only, absent expected rows and the explicit unavailable-slots topology
error mean not ready. Wrong values/schema, duplicates, unexpected IDs and all
other errors fail immediately. The subsequent final exact read has no retries.
Production query behavior remains unchanged.
