# Intent

2026-10-21. Author: Codex; requestor: Albert Wong. Base: retained storage
7debd65 plus main 7a42b2d. Status: private reader implemented and locally verified; public coordinator pending. Refs #362/#359/#360/#363.

Implement the private reader for immutable retained Arrow replay without source catalog bootstrap, preserving
live partitions by default. Validate bounded IPC before Arrow reconstruction;
expiry/revoke/errors must stop reads, while local release never revokes a job.
Global SQL coordination and broader source layouts will follow, and the four
runtime issues remain open until all requirements and evidence are complete.

See [spec](spec.md), [plan](plan.md).
