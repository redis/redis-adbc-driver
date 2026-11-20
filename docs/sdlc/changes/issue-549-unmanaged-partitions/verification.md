# Verification and review

Date: 2026-11-20. Base: `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implementation and verification in progress; no human review or release recorded.

[Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md)

Verification is in progress. Dedicated Software RAM and Flex fixtures use active
4x QPF, six effective Search workers, mnp routing and 32 connections, verified by
the repository setup paths. RAM uses one primary shard under local resource
constraints. Infrastructure names, endpoints, credentials, trust files and raw
logs remain private/ignored.

The first fixed-source fixture incorrectly assumed its filtered application index
would include a NUL-bearing key. Native singleton membership returned zero on
Software RAM; the earlier [Flex unmanaged record](../flex-unmanaged/validation.md)
documents the same limitation. The retained fixture independently checks native
membership for that actual NUL key, requires exact partition agreement, and
preserves its raw HASH/TTL. Non-UTF8 indexed keys must be returned with exact
bytes and values. No generic skip hides the native omission or claims full
NUL-index inclusion support.

The initial Flex preservation test assigned source TTL after calculating its
membership oracle, changing that source's native membership. TTL setup now
precedes the membership oracle, preserving a fixed source during partition reads.

Completed at the measured source before the later cached-layout regression:

- Focused race selection passed on Software RAM and actual Flex. Cases include
  1/4/11-worker exact fixed-source coverage, overlapping prefixes, non-UTF8 keys,
  native NUL membership, exact BIGINT/NULL values, source DUMP/PEXPIRETIME
  preservation, independent processes, source mutation/deletion/expiry, pinned
  registration drop, changed index, server admission expiry and forged descriptor
  expiry, typed parameters and native permission/transport/warning/malformed replies.
- Scoped HASH-reader unit cases accept only direct native WRONGTYPE with an
  accounted pipeline refusal. Wrapped/joined command/outer errors and unaccounted
  native refusals fail without replay.
- Existing managed partition and unmanaged projection/fault regressions passed
  in the selected RAM race run. Unit-only all-package checks and `make build`
  passed; Redis integration cases skip in the unit-only pass.
- [Synthetic comparison](../../../../benchmarks/unmanaged-partitions/report.md)
  completed 18 validated trials across both products at source `222ab181`.
  Four buckets beat one bucket here; ordinary RAM SELECT was substantially faster.
  The measurements do not establish a general speedup or full-scale behavior.

A later new test reproduces cached explicit-layout slices escaping after actual
child-pin loss. #550 owns the minimal shared lifetime validation fix; #549 will
rebase and run combined feature tests before publication. This later regression
is not relabeled as coverage of the measured source.

Skips are the standalone subprocess helper and opt-in profile without its flag;
there is no generic functional skip for NUL membership. Pending: shared fix,
combined final checks, review, PR and fixture cleanup.

The native filtered-index NUL membership investigation is tracked separately in
[#559](https://github.com/redis/redis-adbc-driver/issues/559); source values are
not inferred to be index members from prefix/FILTER-looking fields.

Additional post-measurement regression: explicit unmanaged adoption reads its
single collection prefix, excludes subsequently created nested collections,
preserves source fields, and returns valid empty buckets. The dedicated RAM
`-race` case passed. This does not relabel the measured fixture.
