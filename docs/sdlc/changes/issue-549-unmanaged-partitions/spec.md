# Unmanaged partition contract

Date: 2026-11-20. Base: `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implementation and verification in progress; no human review or release recorded.

See [intent](intent.md), [plan](plan.md), and [verification](verification.md).

- A statement selects `adbc.redis.partition_source_mode=unmanaged` with the default
  deferred `partition_result_mode=live`. The default source remains `managed`.
  Retained/source-mode combinations and managed balanced row-ID planning refuse.
- Accept the existing deterministic row-local partition SELECT subset over one
  existing permanent unmanaged registration. Preserve typed single-row parameters,
  time zone, layouts, native source authentication and full residual evaluation.
- Version-5 descriptors divide arbitrary raw key bytes using the first eight
  SHA-256 bytes interpreted as an unsigned big-endian integer modulo count.
  Return exactly partition_count buckets, including empty/skewed buckets. No
  numeric source key assumption, source field insertion or source mutation occurs.
- Every worker SCANs all registered prefixes, retaining only its assigned keys;
  prefix overlap and duplicate SCAN replies are deduplicated locally. Discovery
  costs O(P*N) and can outweigh parallel HASH/evaluator work. No persisted cursor
  or per-source key inventory is created.
- Native singleton FT.SEARCH INKEYS membership determines inclusion, including
  FILTER and multiple prefixes. Then bounded HGETALL pipelines retrieve exact
  live rows and SQL rechecks the entire predicate. No aggregate cursor/SORTABLE
  requirement is added, including on compatible Software Flex.
- Existing server partition groups retain catalog/registration metadata only.
  Server-enforced TTL and explicit DROP PARTITION SNAPSHOT stop new readers;
  admitted child readers retain their own leases. Refresh/drop can change later
  planning while this group's old registration remains pinned. Index definition
  changes refuse before/after native pages. Same-definition index recreation is
  indistinguishable and follows ordinary live-source readiness semantics.
- Each worker admits at most 100,000 unique assigned keys, 64 MiB of logical
  key/map credits, one-MiB keys, 256 prefixes, one million SCAN calls and 2^31
  observed key replies. Admission precedes map growth. Excess fails explicitly
  with SQLSTATE 53200, never a truncated successful result. SCAN COUNT is a hint;
  these are retained-state/work credits, not a hard bound on native reply
  allocation, Arrow/native values, Redis memory or process RSS.
- HASH fetches skip vanished/retyped rows only from exact successful/native
  evidence. Composed or unaccounted pipeline errors fail without fallback or
  replay; wrapped/joined WRONGTYPE is not an omission proof. Source enumeration
  and reads use execution-owned sockets so cancellation/Close remains isolated.
- Source writes, indexing lag and TTLs may interleave with discovery, membership
  and HASH retrieval. Fixed-source membership is exact and nonduplicated; mutable
  sources have no common point-in-time view or immutable retry guarantee. NUL-key
  behavior must follow actual native source-index membership, including confirmed
  omissions; never manufacture membership from matching HASH fields.

The storage catalog remains format 17; transient partition group records use the
existing lifetime mechanism. Older drivers reject descriptor version5. The new
source selector does not enable unmanaged writes, change registrations, or alter
managed/default partition behavior. Source ACLs still apply at each native read.
