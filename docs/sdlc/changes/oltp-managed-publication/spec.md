# Specification

Date: November 13, 2026. Author: Codex. Source: ca685806. Status: implemented and focused validation complete.
[Intent](intent.md), [plan](plan.md).

- Carry the same authorized dirty descriptors from final index creation into
  readiness checking within one synchronous publication. Preserve metadata SET,
  prefix/cardinality/zero-failure/indexed-ID proofs, and atomic publication fences.
- Pipeline the initial private-directory cardinality and first bounded page.
  Stop exactly at the proven cardinality, retaining the final cardinality check,
  256-row/byte-bounded notifications and all Lua identity/lease/ACL checks.
- Do not issue the no-op abort control command after a confirmed ordinary SQL or
  Arrow commit. Continue aborting failed, unknown, no-op and retained preparation
  paths as before. Pin release and durable outcome resolution are unchanged.
- Reuse the existing atomic clone script's complete base manifest/map/directory/
  reference/lease proof instead of repeating its client-side manifest pipeline.
- No storage protocol, external ownership, TLS, replay or compatibility change.
  Tests must retain unknown-reply, failed pipeline, snapshot isolation and failure
  cleanup coverage. Native before/after results require the same fixtures and
  verified Software 4x QPF; Go helper attribution alone is not native timing.

[Validation and review](validation.md).
