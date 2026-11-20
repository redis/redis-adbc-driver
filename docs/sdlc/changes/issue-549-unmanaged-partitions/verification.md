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

Pending: final checks, measurement, review and cleanup.
