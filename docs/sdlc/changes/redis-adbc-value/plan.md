# README value implementation plan

Date: 2026-10-26. Status: implemented; awaiting PR review.
See [intent](intent.md), [specification](spec.md), and [validation](validation.md).

1. Read the existing README, driver reference, HASH adoption contract, and Python
   integration/status guides; verify the ADBC API overview.
2. Add a concise purpose/value section with explicit capability limits and links
   to the existing detailed guides.
3. Validate the contents anchor and local link/fragment targets, inspect the diff,
   and open a focused follow-up PR from current main.

The earlier canonical-links PR was already merged. This follow-up changes only
the README and these records. Runtime tests and benchmarks are unnecessary.

Review: [PR for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fexplain-redis-adbc-value).
