# Specification

Date: 2026-11-10. Source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.
Status: locally verified. [Intent](intent.md), [plan](plan.md), [validation](validation.md).

Run unmodified Sling 1.6.6 using an explicit Redis shared library, verified TLS,
Redis Software with active/effective 4x QPF and noeviction, and disposable
PostgreSQL. Keep configuration/logs private and clean up only test-owned data.

Use isolated generic ADBC template overrides and a precreated Redis table with
direct truncate-and-load. Test PostgreSQL -> Redis -> PostgreSQL twice and
require exact cardinality plus zero differences in both `EXCEPT ALL` directions.
Cover bigint bounds, duplicate rows, decimal precision, date, microsecond
timestamp, double, boolean, Unicode/multiline strings, empty strings and NULLs.
Record stock full-refresh and empty-result failures separately from successful
configured transfers. Do not claim empty-source support, atomic replacement,
schema equivalence or gated Arrow lane validation.

Expose Sling in the integration listings with these configuration limits. Keep
the historical 50-tool audit and 36-workflow Python read/write sweep labeled by
their actual scope. Register Sling's external CLI runner separately from Python
guide profiles; documentation inventory checks must cover both runner types.
