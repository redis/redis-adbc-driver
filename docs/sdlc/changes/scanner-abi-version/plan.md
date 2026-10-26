# Scanner ABI version: plan and evidence

Date: 2026-10-21. Source: 7025103. Status: local verification passed.
See [intent](intent.md) and [spec](spec.md).

1. Inspect failed job logs and update the clean checkout to merged fixes.
2. Rename the action input environment variable and all its shell references.
3. Reproduce CMake version selection before/after and check action syntax.

Evidence:

- Failed job 119377899052: Python analytics reported 1 failed, 24 passed,
  2 skipped; scanner loading required `v2.0.0` but received `2.0.0`.
- Configured the available DuckDB v2.0.0 source (commit d54cbbe3df) in separate
  temporary build directories, both with `-DOVERRIDE_GIT_DESCRIBE=v2.0.0`.
  With `DUCKDB_VERSION=2.0.0`, CMake reported version/folder `2.0.0`.
  With that variable unset and `SCANNER_DUCKDB_VERSION=2.0.0`, it reported
  version/folder `v2.0.0`. Both configurations completed successfully.
- actionlint 1.7.12 passed `.github/workflows/test.yml`; composite action YAML
  parsed successfully and its extracted build script passed `bash -n`.
- `git diff --check` passed.

Review: all four shell references use the renamed variable; input validation
and exact tag selection are preserved. No assertions or skip guards changed.
No full scanner rebuild or hosted integration rerun is claimed by this record.
Existing cluster-readiness and native loader fixes were already merged and
are outside this patch.
