# Intent: reliable release cluster startup

Date: 2026-10-20. Source: `11faf538`. Requestor: repository user in Codex.
Status: implementation. Author: Codex; no human review implied.
[Spec](spec.md), [plan](plan.md), [validation](validation.md).

The release validation for #338 failed on amd64 when its first cluster ACL
connections returned CLUSTERDOWN and adopted-table queries returned partial
rows. The corresponding eight-job Test matrix passed. Harden the test fixture's
readiness gate and preserve useful diagnostics; do not skip failing tests or
change driver query behavior to accommodate an unhealthy cluster.
