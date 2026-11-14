# Specification

Date: November 14, 2026. Author: Codex. Status: prototype.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

1. After the unchanged manifest/ZCARD/HLEN/valid commands, queue one NoRetry
   EVAL_RO on the same declared metadata-root key in that acquisition pipeline.
   Skip shared/restricted-reader pins, restricted stores and contexts without the
   matching store/pin/committed root. Known restricted modes retain their original
   native path without probing additional commands.
2. Before HGETALL, atomically check native HLEN <=256 and full MEMORY USAGE
   SAMPLES 0 <=64 KiB. Keep command/key ACL checks for HLEN, HGETALL and MEMORY.
   Bound before enumeration, rather than checking an already materialized Lua
   reply. A direct fixed size/permission-decline token or exact native EVAL_RO
   command denial leaves the original separate native HGETALL path in place.
   Never fall back on untyped, wrapped/joined, altered or unknown outcomes.
3. Preserve the original metadata and lease error precedence. An unaccounted
   pipeline failure is terminal, even with populated replies. Interpret a
   successful native lease reply locally before inspecting new map data. Defer
   lease digest repair until all new replies/shapes have been checked; a later
   unknown or malformed map cannot cause an earlier known refusal to replay.
4. Validate map shape/cardinality, and install it only after every original proof
   and lease check succeeds. The existing resolver consumes this same immutable
   per-pin map and initializes its existing dependency/proof state. Native
   descriptor GETs/ACLs, presence proofs, final validation and buffered-batch lease
   checks remain. A fresh scope/pin cannot reuse another scope's map.

Resource proof: [MEMORY USAGE documentation](https://redis.io/docs/latest/commands/memory-usage/)
requires SAMPLES 0 to inspect all nested values; a five-entry estimate is unsafe.
The [Redis 8.8 HASH allocation implementation](https://github.com/redis/redis/blob/8.8/src/t_hash.c#L1364)
and [object sizing](https://github.com/redis/redis/blob/8.8/src/object.c#L1313)
account for the full HASH allocation. Older HASH tables sample every entry with
SAMPLES 0; listpacks report their allocation. Entry count is checked first. The
memory guard bounds enumeration even if externally corrupted metadata includes a
large field/value; it is not a promise of precisely 64 KiB of Lua VM overhead.
Only the metadata root is eligible; arbitrary application HASHes are unaffected.
