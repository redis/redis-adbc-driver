# Intent: include latest Airflow hook evidence

Date: 2026-10-29. Requestor: repository user. Author: Codex.
Base: public main `71625e1`. Status: prepared for review.
See [spec](spec.md) and [plan](plan.md).

Follow merged comparison PR #225 with the evidence from merged hook PR #232.
Make tested helper APIs and provider limitations clear. Documentation only;
no new integration runs or behavior changes.

Follow-up: incorporate merged PRs #234/#235 on main `f728a66`. Document
the append-only dlt destination and pandas writes; distinguish historical live
dlt evidence from public-base skips and avoid additive pandas suite counts.
