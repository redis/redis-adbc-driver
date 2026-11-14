# Specification

Date: November 14, 2026. Author: Codex. Base: 0b216478.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

Only a single-key, non-strict managed writer-scope lookup may use the new native
read script. Each request declares the exact HASH and its corresponding directory
in the existing metadata slot. It reads precisely the same HGETALL or requested
HMGET fields as before. Only an absent row triggers the original count (published
segments) and membership checks; private-owned directory rules remain unchanged.
The caller resolves newest to oldest and stops at the first image or tombstone.

- Keep all external pin, source validation, writer and publication fences.
- Preserve row IDs and int64/decimal/binary values as strings, never Lua numbers.
  The optional cardinality proof is eligible only below the exact-double bound.
- At most 254 requested fields and one MiB of request key/argument bytes; larger
  requests retain the native path. Full images retain the existing managed row
  image bounds. No multi-row or strict direct-reader behavior change.
- The script contains no mutations. Unknown read replies fail without replay.
  Only a definite top-level script permission refusal may select the old native
  commands. Inner HASH/directory failures are prefixed and remain terminal.
- No new storage records, protocol version, user option or platform scope.

Acceptance requires exact large integers, NULL/missing fields, newest wins,
tombstones, private updates, early termination, absent/corrupt row/directory/type
refusals, ACL fallback versus inner failure, no replay and strict/bulk fallbacks.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
