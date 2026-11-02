# Integration acceptance intent

Date: 2026-11-01. Requestor: Albert Wong. Source: `69482fb` (`origin/main`).
Status: implemented; verification in [validation](validation.md).

Audit each documented integration for an upstream acceptance contract and reuse
applicable suites in the testing harness. Keep dbt and Airbyte testing in their
dedicated projects, as explicitly requested during this change.

The detached starting checkout diverged from current main; fetched origin and
created `codex/integration-acceptance` from origin/main without merging the older
history. See [spec](spec.md) and [plan](plan.md).
