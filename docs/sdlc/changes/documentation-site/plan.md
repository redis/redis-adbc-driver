# Plan: public documentation website

Date: 2026-11-10. Source: `73eb9642`. Status: implementation.
Requestor: repository user. PR: [#368](https://github.com/redis/redis-adbc-driver/pull/368).

1. Add pinned MkDocs dependency, explicit navigation and staged Markdown input.
2. Rewrite repository links without publishing their targets; add crawler files.
3. Add strict build, site checks and GitHub Pages deployment workflow.
4. Build locally, inspect generated HTML and verify publication scope.
5. Publish the change through GitHub and enable Pages when access permits.

Local build and link checks passed; see [validation evidence and agent review](validation.md).
GitHub Pages enablement needs the requestor's settings change because the CLI
token cannot enable it. No Redis tests are required for this documentation-only
change. See [intent](intent.md) and [specification](spec.md).
