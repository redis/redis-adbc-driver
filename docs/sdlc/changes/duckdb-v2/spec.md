# Contract

Date: 2026-10-20. Author: Codex. Status: implemented and locally verified;
remote CI and review pending.
See [intent](intent.md), [plan](plan.md), and [verification](validation.md).

- Pin DuckDB to the registry's 2.0 commit
  d54cbbe3df17e8e5ce68312f483b4155836112aa and extension-ci-tools to
  78bd1e4abbd543822166d5b07644751ba1fc6e8d on v2.0-cyanoptera.
- Build the extension and matching 2.0 CLI/test runner on native Linux amd64,
  Linux arm64 and macOS arm64. Explicitly stamp v2.0.0 for shallow checkouts.
- Run offline SQLLogicTests with the 2.0 runner's --fail-require-skip flag and
  require positive passed-test counts and zero skipped tests in CI.
- Use the matching 2.0 CLI for real Redis runtime checks on Linux. Do not
  require a separately released Python DuckDB wheel for this engine commit.
- Independently check physical column types and all values in 10,003 rows,
  as well as existing local query, prepared execution and error scenarios.
- Preserve the standalone scanner's existing SELECT-only contract. No driver
  SQL/storage behavior changes or new platform claims are part of this work.
- Keep the registry descriptor limited to repo.ref for 2.0. Do not add a
  variegata deployment. Historical 1.5 validation records remain historical.
