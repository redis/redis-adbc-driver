# Plan

Date: 2026-11-23. Origin: follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606).
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Add conservative shape/aggregate eligibility and bounded dimension hashing.
2. Stream fact pages into existing exact grouped accumulators.
3. Compose guarded UNION projections without materializing the fact relation.
4. Require parsed normal-presence and weekly CASE selection regressions.
5. Test duplicate/NULL bags, provenance, aliases, source failures, cancellation,
   conservative fallbacks and retained-context no-consumption behavior.
6. Run standalone unit checks. Record native query evidence separately before
   claiming performance or issue resolution; keep full suites stopped.
