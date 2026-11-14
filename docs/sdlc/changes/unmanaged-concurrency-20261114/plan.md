# Plan

[Intent](intent.md), [specification](spec.md), [validation](validation.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

1. Attribute the actual guarded reader against native HGETALL at one/four
   clients on the owned 100K fixture, retaining every observation.
2. Replace per-row Lua with native HMGET: non-NULL values prove existence;
   all-NULL vectors select a complete later native HGETALL image.
3. Check exact int64/binary/empty values, all-NULL and zero-field rows, live
   update/delete/type changes between observations, request bounds, command
   permissions, direct WRONGTYPE and every unknown/malformed pipeline outcome.
4. Use the coordinated Redis Software slot only after verified 4× QPF; compare
   actual Go ADBC full SQL one/four-client guarded baseline and native candidate
   with identical fixtures/queries and complete typed Arrow/oracle validation.
5. Retain all observations, slower cases and source identities. Endpoint INFO
   CPU/cached statistics are unavailable attribution, not zero server cost.
   Keep operational logs/identifiers private and close every client before
   yielding the slot. Native Python/C-boundary matrix remains parent work.
