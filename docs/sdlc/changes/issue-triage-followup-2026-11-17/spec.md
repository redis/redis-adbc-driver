# Scope and acceptance

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source: `19ebcb4605143974495b3e83791c2909b37b7809`. Status: implementation and verification complete; PR pending.

Inventory all 51 open issues, excluding #475 from implementation. Verify issue bodies/comments, current source and relevant upstream outcomes. Retain unresolved acceptance rather than closing broad trackers for partial work.

Correct #110’s stale OVERLAPS/pg_input_error_info inventory and add independently scoped MIN_SCALE support with exact integer result, numeric signature checks, NULL and boundary tests. Complete #273’s packaged worker-local Dask Arrow sink with explicit ownership, serialized writes, acknowledged counts, partial failure and task-recomputation duplication semantics, including separate-process validation.

Use owned Redis Software with verified effective 4x QPF for integration checks. No ClickBench, unsupported distributions, transaction/storage-protocol changes or upstream submissions.

See [intent](intent.md), [plan](plan.md), [dispositions](triage.md).
