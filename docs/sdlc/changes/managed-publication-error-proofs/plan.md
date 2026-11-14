# Plan

[Intent](intent.md), [specification](spec.md), [validation](validation.md).
Parent context: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

1. Retain the preceding real-validator RED regressions and complete performance
   experiment evidence, including slower cases and unselected source identities.
2. Split a clean safety-only patch from PR #439 source: two direct native
   assertions, private publication transient classifier and real-validator tests.
3. Explicitly exclude snapshotFirstIndexReadiness, batching runtime changes,
   queue-only helpers/tests and diagnostic hook changes from integrated code.
4. Have the coordinator run unit/race and relevant Redis Software checks on the
   actual integrated source, using verified 4× QPF and serialized fixture access.
5. Independent review and PR links; maintainer approval and merge remain separate.
