# Canonical repository links

Date: 2026-10-26. Requestor: Albert Wong. Status: proposed for review.
Source baseline: `2f053d3affad90a1d2798c000f0d1825183e6e54`.

After migration, project entry points and documentation still link to the former
personal repository. Issue numbers changed during migration, so changing only
the repository owner would point readers to unrelated issues.

Update project, documentation, release, and issue links with migrated equivalents
to `redis/redis-adbc-driver`. Preserve original destinations for historical pull
requests, commits, workflow runs, and captured evidence without migrated copies.
Published package versions, driver behavior, and release assets are outside this
change. Package URL metadata takes effect in a future package publication.

See [specification](spec.md), [plan](plan.md), and [validation](validation.md).

Review: [pull request for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fupdate-redis-adbc-links).
