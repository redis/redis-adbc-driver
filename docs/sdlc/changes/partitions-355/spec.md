# Partition API contract

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: a08573bdc40c07bca5bc3ccf9cee1e59cbc6490c.
Status: implementation contract; human review pending.

See [intent](intent.md), [plan](plan.md), and the full
[consumer contract](../../../partitioned-reads.md).

Support single-table permanent managed reads with column projection and simple
row-local predicates. Reject global operations and expressions that require
session execution. Capture allocated row-ID high watermark and divide into at
most the requested count of disjoint numeric ranges below 2^53.

Descriptors are versioned JSON bytes carrying original SQL, resolved relation,
time zone, physical metadata fingerprint and range. Validate bounded input,
query scope, target identity and worker permissions. No credentials, pointers,
retained rows, signature/capability semantics or expiry timer. Future writes
can change results; no shared snapshot. Fresh IDs beyond the watermark do not
belong to the planned result.

Reuse existing cursor/Arrow stream lifecycle. Reader release, context/connection
cancellation and connection close terminate streams. Require unrestricted Search
aggregate cap rather than using a materializing fallback. Return Arrow schema
even when empty, and unknown affected count (-1).

Verify fixed-data row multiset and schema equivalence, empty/sparse partitions,
direct-key predicates, cross-process consumption after planner close, invalid
and stale descriptors, ACL denial, cancellation, early release, and C/Python
ownership. Supported deployment claims remain bounded by actual evidence.
