# Plan

1. Bind a fused pointer-input function for common compatible primitive outputs;
   retain generic typed coercion/append and the original byte-budget path.
2. Differentially verify every output/error against existing binding across
   NULLs, range errors, aliases, NaN, temporal, binary and uncommon fallbacks.
3. Verify complete map/positional wide construction, defaults, retained ownership,
   OFFSET/LIMIT/error ordering and terminal partial-row cleanup.
4. Parent schedules unit/race then verified Software 4x QPF/TLS checks and fresh
   baseline/candidate full native matrix; do not access shared fixture otherwise.
5. Record source/build identities, every sample and limitations. Parent review
   decides eligibility before any publication; no human approval is implied.
See [intent](intent.md) and [spec](spec.md).

Review: [PR #431](https://github.com/redis/redis-adbc-driver/pull/431) is open and unmerged; no human approval or release is claimed.
