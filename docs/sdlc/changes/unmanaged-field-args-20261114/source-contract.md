# Exact unmanaged source retrieval: remaining contract

Date: 2026-11-14. Local baseline `b904cf8d`. This is design/source analysis,
not a new Redis Software execution result. [Intent](intent.md), [spec](spec.md),
[validation](validation.md). Existing request:
[Search #12334](https://github.com/RediSearch/RediSearch/issues/12334).

The remaining high-value boundary is moving exact source projection or exact
reduction into Search's existing batched candidate traversal. Filtered unmanaged
reads currently discover candidates in Search and issue one HMGET per candidate.
Grouped/full scans already retain batched `LOAD *`; replacing that path with
100,000 separate source reads would increase fanout rather than solve it.

`LOAD` exists and can fetch source fields. It is insufficient to assume that
`LOAD @field AS alias` forces exact authoritative bytes. In the inspected upstream
source at [`33d90342`](https://github.com/RediSearch/RediSearch/tree/33d90342bc9c601d904fb58446f8bda4eaba5011),
[`processLoadStepArgs`](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/pipeline/pipeline_construction.c#L402)
passes the original source path to lookup creation even when an alias is used.
[`get_key_load_slot`](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/redisearch_rs/rlookup/src/lookup.rs#L487)
can mark the aliased key from that schema field and return no document load when
its sorting-vector value is available. The
[HASH loader](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/redisearch_rs/rlookup/src/load_document/hash.rs)
also distinguishes string and numeric coercion, with numeric field loads passing
through double. These are source observations on a pinned upstream revision;
they do not establish that this revision is installed in the test fixture or
that all released versions behave identically. The local driver's existing
`loadsExactly` guard remains authoritative for supported planner choices.

A useful source-projection API must offer an explicit source-bytes mode that
bypasses sorting vectors and numeric coercion for each selected field. It must
return a per-row HASH existence/type result even when all selected fields are
absent; selected NULLs alone cannot distinguish an existing row from a deleted
key. It must preserve binary values, full signed int64 text, application membership,
ACL enforcement, cursor limits, cancellation and warning/error signaling. Discovery
and source reads may remain live, provided the observation boundaries are explicit.
No unrelated application writes may be presented as a statement snapshot.

An exact reducer additionally needs full signed int64 grouping/comparison, missing
field versus zero distinction, malformed/out-of-range signaling, mixed-sign exact
SUM with defined overflow/result representation, and a defined distributed merge.
Validation must include adjacent values above 2^53, both int64 endpoints, cancellation
where double accumulation loses low bits, all-NULL rows, deletion/type replacement,
and source changes between statements. COUNT(*) must count the authoritative live
row observations rather than blindly reuse an index hit count. These requirements
are already within #12334's scope; no duplicate upstream issue is needed.

An unverified alias, registration-time range check or approximate numeric reducer
cannot satisfy that contract for externally writable HASHes. Lua would introduce
separate ACL, materialization, arithmetic and execution-budget work without giving
an exact distributed Search reducer. The small local field-argument optimization
therefore reduces client work only; it leaves this server/API boundary visible.
