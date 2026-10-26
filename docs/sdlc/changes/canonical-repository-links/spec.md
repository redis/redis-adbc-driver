# Link migration specification

Date: 2026-10-26. Status: proposed for review.
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- Project and package homepage, documentation, issue-index, and release links use
  `https://github.com/redis/redis-adbc-driver`.
- Explicit issue links in Markdown and source comments use the verified migration
  mapping, including updated issue numbers in Markdown link labels.
- Bare migrated issue references in the README and current reference/design guides
  become explicit links to their destination issues; code examples remain intact.
- The README identifies v0.0.18 as the release containing prebuilt binaries. Older
  migrated release pages intentionally have no uploaded assets.
- Original PR, workflow-run, and commit links remain attached to their actual
  historical records. Captured JSON evidence and differential corpus provenance
  remain unchanged, including their original issue identifiers and SQL fixtures.
- No runtime behavior, dependencies, package version, measurements, claims of
  supported platforms, or workflow permissions change.

Acceptance requires validating package TOML, checking every changed canonical URL
against local documentation or live migrated issue/release metadata, and checking
that the Go change affects only a comment. Runtime Redis tests are unnecessary for
this link and metadata change.

Review: [pull request for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fupdate-redis-adbc-links).
