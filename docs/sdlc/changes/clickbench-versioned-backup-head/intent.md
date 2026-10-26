# Resume exact-version backup verification

2026-10-21. Codex, within Albert Wong's authorized full-backup workflow. The runner received S3 403 for version-specific HEAD while saving the checkpoint backup. The operator profile could verify the version, and the runner could HEAD the current object with the same version ID, SHA256 metadata and byte count.

Preserve strict snapshot identity without modifying the reused IAM role. See [spec](spec.md) and [plan](plan.md).
