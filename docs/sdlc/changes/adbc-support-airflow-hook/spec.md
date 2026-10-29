# Specification: Airflow support scope

Date: 2026-10-29. Author: Codex. Base: `71625e1`.
See [intent](intent.md) and [plan](plan.md).

Link #232 and its exact recorded environment. Distinguish eight total checks
from five focused hook cases and the earlier two TaskFlow cases. Describe the
explicitly imported Redis hook, generic provider transaction-mode failure,
SQLExecuteQueryOperator selection and inherited insert autocommit requirement.
Retain untested remote, Flight SQL, live hook TLS, Linux and Airflow 2.x limits.

Follow-up: incorporate merged PRs #234/#235 on main `f728a66`. Document
the append-only dlt destination and pandas writes; distinguish historical live
dlt evidence from public-base skips and avoid additive pandas suite counts.
