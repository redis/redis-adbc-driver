# Intent

2026-10-21. Author: Codex; requestor: Albert Wong. Base: `9510b9d` (includes
merged #424 / `5c25bad`). Status: implemented; verification and PR review.
Refs #359/#360/#362/#363. The requestor explicitly requires runtime features.

Enable opt-in retained ExecutePartitions: execute global SQL once over bounded,
authoritatively captured source rows and publish immutable final Arrow chunks.
Support exact large IDs and application-owned layouts without source migration.
Independent authenticated workers must replay after producer closure/death;
quota, lifetime, partial publication and cancellation must fail safely.

Preserve live mode as default. Do not claim an atomic Redis source snapshot,
exactly-once sinks, unrestricted SQL, process-wide/RSS limits or production
performance. Redis Cloud Pro remains the default for new Cloud deployments.

See [spec](spec.md), [plan](plan.md), [evidence](evidence.md).
