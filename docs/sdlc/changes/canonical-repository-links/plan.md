# Canonical link implementation plan

Date: 2026-10-26. Status: implemented locally; awaiting PR review.
See [intent](intent.md), [specification](spec.md), and [validation](validation.md).

1. Inventory references to the former repository and classify migrated destinations
   separately from historical records that have no counterpart.
2. Use the verified migration mapping to update issue URLs and their displayed
   numbers in Markdown, plus the existing regression-test comment.
3. Update Python package URLs and README/documentation/release entry points.
4. Validate canonical destinations and metadata, inspect the diff, and open a PR.

The changes apply to tracked text documentation, package URL metadata, and one
comment. The private migration export and operational cleanup details are not
included in the public PR. Human review is requested; no merge is authorized by
this record.

Review: [pull request for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fupdate-redis-adbc-links).
