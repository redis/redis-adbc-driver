# Airbyte support refresh validation

Date: 2026-11-01. Base: `d1ed98f`. Author: Codex; human review pending.

- Read Airbyte PR #90323: open draft, head
  `5c20a3f6e218d27c3e2f889e3cec53dbf8449453`.
- Read its pinned `docs/integrations/sources/redis-adbc.md`; registry, type,
  full-refresh and backend limitations match the updated comparison.
- The reported 54 connector tests, two PostgreSQL syncs and separate CDK
  3 passed / 19 skipped / 18 deselected are upstream evidence, not rerun here.
- Read `redis/redis-dbt-adapter` README and `v2/VALIDATION.md`; the v2 engine
  remains in `alberttwong/dbt`. The migrated historical Actions URL returned
  404, so cite the current validation record rather than invent a migrated run.
- Table accounting: 50 rows; 39 qualifying, 35 ADBC, 25 Flight SQL. Airbyte is
  read-only on its ADBC source path and does not add a Redis Flight SQL badge.
- Documentation whitespace and link/shape checks passed. No integration suites
  were rerun for this documentation change; historical tests retain their scope.
- Native deck write/readback and a 24-page PDF export passed. Inspected the
  rendered contact sheet and the new Airbyte slide at full size; no overflow
  or clipped text found. New slide `redis_airbyte_source_20261101` follows dbt.
- Review of correctness, security/ownership and evidence scope found no
  actionable issues in this documentation-only diff. This is agent review,
  not human maintainer approval.

Deck: https://docs.google.com/presentation/d/1CdXUMzl7VJNHgVke0hR0rl8n3TmqzcV19gNrnT8l-d8/edit

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
