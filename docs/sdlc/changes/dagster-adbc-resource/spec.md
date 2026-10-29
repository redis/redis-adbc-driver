# Specification: community resource

Date: 2026-10-29. Source: `da1e7c1`. Author: Codex. Status: verified locally.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

The published ADBCResource must load the native Redis library directly and via
an isolated `redis` manifest, forward the default-schema database option, ingest
and fully fetch Arrow data including Unicode and NULLs, bind query parameters,
and persist writes across connections. Dagster must inject the resource into
dependent materialized assets. An exception inside the connection context must
close the connection and permit a new connection.

Use explicit `autocommit=True`, since Redis has no transactions. The community
resource defaults to False; driver-manager 1.13.0 warns and continues in
autocommit mode. Tests own unique schemas and drop only those schemas in teardown.
No TLS, cluster, transaction or platform-wide compatibility claim is made.
