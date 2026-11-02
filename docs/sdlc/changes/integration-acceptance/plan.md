# Integration acceptance plan

Date: 2026-11-01. Source: `69482fb`. Status: implemented.
Related: [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Fetch origin, use current main, inventory guides/comparison and runner.
2. Inspect upstream suite sources and record applicability per tool.
3. Add isolated suite bootstraps, strict reporting, Make/candidate entry points
   and manual Actions evidence upload. Leave dbt/Airbyte to their projects.
4. Verify failure paths, audit completeness, shell/Python/YAML syntax, and
   live suite execution on an owned TLS Software fixture with verified 4x QPF.
5. Record actual failures/skips, review the final diff, and remove the fixture.

Live setup required making the copied public CA readable by the container's
Redis CLI user. Native test interpreters must also start with the Redis CA
already in their environment, while uv downloads retain ordinary system trust.
Fivetran SDK 2.13 moved the tester helper into its common package; support both
module locations without changing tester assertions.
