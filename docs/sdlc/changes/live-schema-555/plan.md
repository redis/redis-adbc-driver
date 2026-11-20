# Plan

Date: 2026-11-20. Source base: `27e5a8d2`. Author: Codex. Issue: [#555](https://github.com/redis/redis-adbc-driver/issues/555).

1. Add explicit evolution API and CLI with compatible selected schema planning.
2. Add bounded exact HASH validation and durable shadow-index/resume/cancel states.
3. Keep new SQL blocked during transitions via existing identity checks; preserve
   source HASHes, old active index until switch, and recoverable cleanup identity.
4. Validate pure planning, source/TTL preservation, exact values and binary keys,
   incompatible inputs, interrupted transitions, cancellation, stale identities,
   CREATE refusal recovery, and aggregate pipeline reply loss.
5. Update live/ownership/feature documentation and record actual verified Software
   QPF4 evidence. No benchmark, managed storage change or Flex support expansion.

See [intent](intent.md), [spec](spec.md), and [validation](validation.md).
