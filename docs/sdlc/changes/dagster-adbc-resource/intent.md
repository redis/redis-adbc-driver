# Intent: community Dagster ADBCResource compatibility

Date: 2026-10-29. Source: `95d6583`. Requestor: repository user in this
Codex chat. Author: Codex. Status: implemented and locally verified.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Test the published community `dagster-adbc` ADBCResource with the Redis driver,
independently of the repository's RedisADBCIOManager. Retain reproducible tests
and document working configuration. No driver behavior or release changes.
