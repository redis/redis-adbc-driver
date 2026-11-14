# Plan

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439). Date: November 14, 2026. Requested by Albert Wong; implemented by Codex.

1. Inspect the sole failing job and reproduce the unchanged test under race.
2. Schedule the real reopened worker during the existing bounded poll.
3. Run repeated verified Software race checks and independent source review.
4. Push the test-only correction and verify remote CI.

[Intent](intent.md), [spec](spec.md), [validation](validation.md).
