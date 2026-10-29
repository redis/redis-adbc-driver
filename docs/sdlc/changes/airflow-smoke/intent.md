# Intent: test Redis ADBC with Apache Airflow

Date: 2026-10-29. Source: `95d6583`. Requestor: user in this chat.
Author: Codex. Status: implemented, local verification.
[Spec](spec.md), [plan](plan.md), [evidence](validation.md).

Demonstrate an actual Airflow DAG loading and querying Redis through the current
driver, with independently checked results and cleanup. Include the official
Arrow provider where feasible and report its compatibility limits.

Scope excludes deploying Airflow, remote executors, releases, and a new provider
package. No issue or PR was supplied or created; no human approval is implied.
