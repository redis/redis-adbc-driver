# Plan and validation: parallel Superset CI

Date: 2026-11-04. Source: `85a9bf3`. Status: implemented; CI execution pending.
Author: Codex. Requestor: Albert Wong.

1. Inspect current upstream and preserve its helper job and fixture contracts.
2. Move the existing Superset step into an independent amd64 job with its own
   native build, verified Software fixture, failure logs and cleanup.
3. Run actionlint, diff whitespace checks and coverage-selector unit tests.
4. Open a PR; verify integration execution and elapsed-time effect in Actions.

Validation results will be recorded in the PR. Local lint and unit checks cannot
prove the hosted integration or a speedup. Review inspected job isolation,
unchanged test commands, TLS and QPF setup, cleanup, and unchanged permissions;
no actionable findings. Human review and hosted integration remain pending.

See [intent](intent.md) and [specification](spec.md).
