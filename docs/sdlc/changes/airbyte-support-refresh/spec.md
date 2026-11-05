# Airbyte support refresh specification

Date: 2026-11-01. Status: documentation only. Base: `d1ed98f`.

The Airbyte row qualifies via the implemented full-refresh Redis ADBC source.
It is a draft community proposal, disabled in Cloud/OSS registries, and reads
driver-exposed tables/views with Search and autocommit. It emits JSON/stdio;
no native Arrow transport, performance claim, CDC or shared snapshot is implied.
The SQLite Flight SQL reference test does not establish a Redis Flight SQL path.
The separate marketplace destination does not validate Query Engine access.

Cite externally recorded tests and PostgreSQL destination syncs separately from
this driver's historical native sweep. Keep AMD64, deployed-platform and security
combination gaps visible. Update dbt project links while retaining the separate
v2 engine fork and historical validation scope.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
