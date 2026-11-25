# Intent

2026-11-25; base `268ccf03` (main). Requested by the TPC-H issue work for
[#631](https://github.com/redis/redis-adbc-driver/issues/631): a 1,000-term numeric key
union took 8–9× as long as a range returning as many rows, with the same commands and bytes.
Status: local change, not released. See [spec](spec.md), [plan](plan.md),
[validation](validation.md).

The driver builds numeric and TAG unions for `IN` lists and subqueries, native join lookups
(`nativeJoinLookups`, up to 1,000 keys), semi-join index terms (`semiJoinTerm`) and semi-join
page probes (256 keys, #630). On a managed snapshot table, `snapshotCandidateScan` reads a
filter's candidates in bounded row-ID windows `(query) @__rowid:[(after hi]` (#599, #634).
Measured on raw Search (see validation), one evaluation of a union costs about 40–50 µs per
term, roughly linearly, and **every window evaluates the whole union again**. A 1,000-key
lookup paged through about twenty windows; a range paid its small cost per window instead.

Read a large union's candidates with one or two Search evaluations per source, without
changing results, visibility, ordering, identity checks or the windowed path for every
other query. Out of scope: coalescing keys into ranges, splitting unions into bounded
batches, a TAG mirror of integer keys, the 1,000/4,096 union term caps, and Flex (its
candidate paths already read complete sets, #644).
