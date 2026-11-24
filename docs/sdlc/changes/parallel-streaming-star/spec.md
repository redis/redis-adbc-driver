# Specification

Date: 2026-11-23. Status: local candidate.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

After the existing conservative star-shape and dimension preparation, admit only
broad direct managed native facts under a shared statement pin. Exclude private
owned sources, derived/memory/UNION facts, cleanup-pending layouts, selective
Search queries and explicit key requests. Preserve the complete fact predicate;
selective paths retain their existing scan to avoid repeated candidate discovery.
Use the existing pinned row sequence as a partitioning upper bound, never a row
count. Direct missing sequence capability may retain the existing serial path;
wrapped/joined missing replies and other failures fail closed before output.

Acquire one existing worker-budget grant, including a grant of one. Honor queue
cancellation and other owners' tokens. Use disjoint exact int64 directory ranges,
with a final unbounded int64 tail and no floating Search row-ID filters. Each
worker clones its executor/cache, owns its source iterator/environment and merged
maps, and reads only immutable prepared dimension buckets. Preserve duplicate
bags, NULL nonmatches, requested provenance, and the original exact aggregate
and CASE evaluators. Group accumulators are never mutated concurrently.

Each worker has one queued page and one page under construction, at most 256
joined rows each, plus its existing bounded source page. A single coordinator
drains workers in range order into the original reducer, preserving ascending
fact order and group discovery. Dimension and group-state memory retain their
existing cardinality dependence; source pages and intermediate output grow only
with the admitted worker grant, not full fact or join result cardinality.

Remember a reduction callback error, suppress later callbacks/probing and finish
bounded authoritative source scans so source failures retain precedence. Ordinary
source errors are selected deterministically by range while peers drain under
the shared statement deadline. Cancellation aborts workers and blocked sends;
join every worker and close every source before returning. Validate the shared
pin before success or a reduction error; release the grant after worker join and
lease handling on every path. No replay or midstream fallback is permitted.
