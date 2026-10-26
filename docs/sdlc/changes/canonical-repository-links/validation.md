# Canonical link validation

Date: 2026-10-26. Status: checks passed; human PR review pending.
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

Validation covers changed canonical URLs and their corresponding migrated issue
numbers, package TOML parsing, local documentation and fragment targets, preserved
historical URLs, comment-only Go changes, and whitespace checks.

No Redis tests or ClickBench runs are required: this change modifies links and
package URL metadata without changing driver behavior. Existing published wheels
and their metadata are unchanged by a repository PR.

Review: [pull request for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fupdate-redis-adbc-links).

## Results

- Verified 83 unique canonical URLs: 75 issue URLs against migrated issue titles,
  three documentation URLs against local files/fragments, two release URLs against
  live migrated releases, and three project entry points.
- Confirmed displayed issue numbers match their mapped URL destinations.
- Preserved all 14 historical PR/commit/workflow links present in changed files.
- Parsed python/pyproject.toml and confirmed only project.urls changed; package
  version, build configuration, dependencies, and entry points are identical.
- Confirmed the Go edit changes only a regression-test comment; ran gofmt.
- Confirmed SQL/JSON evidence, corpus fixtures, and captured historical metrics
  are unchanged. No runtime tests or benchmarks were run.
- git diff --check passed. Checked changed public files for credentials and
  private operational cleanup details; none were introduced.
- Agent self-review found no runtime or historical-evidence changes. Human review
  has not occurred, and no approval or merge is implied.
