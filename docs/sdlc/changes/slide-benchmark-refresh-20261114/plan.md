# Plan: performance-slide benchmark refresh

[PR #442](https://github.com/redis/redis-adbc-driver/pull/442).

See [intent](intent.md) and [spec](spec.md).

1. Create an isolated branch at current main; preserve unrelated working trees.
2. Provision an owned Software fixture through the repository helper and verify effective QPF/topology/TLS. Build a pinned native Linux client and exact versioned driver/CLI before timing.
3. Run the unmodified bulk and scenario timer/validation bodies consecutively. An external wrapper records the bulk owner marker outside timing so inter-suite cleanup can verify ownership. Avoid concurrent heavy test/render work.
4. Audit full coverage and measured arithmetic, generate charts from the same driver binary, record source/harness/runtime/resource identities and privacy-review all public artifacts.
5. Update current performance slides and verify exported numeric content and visual layout. Keep archived ClickBench comparisons unchanged.
6. After all clients end, verify marker ownership, empty the database, remove owned containers/network/image tag and generated TLS/credentials; verify preservation of preexisting resources.
7. Finish validation/review, commit evidence, push the branch and open a PR. Link the final deck and remaining limitations.
