# Ray Data integration smoke test

Date: 2026-10-29. Source baseline: `95d6583`. Requestor: user in this Codex
chat. Author: Codex. Status: locally verified; no maintainer approval implied.

Test the existing Redis ADBC driver with Ray. Exercise public Ray Data SQL
read/write APIs and Arrow interchange against disposable Redis Software.
Preserve reproducible opt-in tests and document observed limits. No driver
behavior change, release, benchmark or remote cluster deployment is requested.

See [specification](spec.md) and [plan and evidence](plan.md).
