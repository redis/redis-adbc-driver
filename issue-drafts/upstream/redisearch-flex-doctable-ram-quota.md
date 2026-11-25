Target: RediSearch/RediSearch — new issue (bug_report.md template: "[BUG]" prefix, label `bug`; feature_request.md also fits if maintainers see this as sizing guidance). Alternatively the internal Redis Software/Flex channel.
Title: [BUG] Search on Flash: DocTable metadata fills the Flex RAM quota during a large HASH index backfill (OOM at ~31M documents)
Status: facts match #577 and its record. Before posting: confirm the host/OS (not recorded), and use an account with RediSearch issue permissions (an earlier attempt was rejected). Then record the URL in #577.
---
**Describe the bug**

On Redis Software with Redis Flex and Search on Flash, an index over a large set of existing HASHes stopped at about 31M documents. The client's next command then failed with:

```
OOM command not allowed when used RAM > bigredis-max-ram
```

At the last `FT.INFO` sample, `doc_table_size_mb` was 4044.37, about 137 bytes per indexed document by arithmetic. That is almost the whole 4 GiB Flex RAM allocation. `key_table_size_mb` was 0, and server process memory was far below its container limit. We found no documentation of which Search metadata stays in RAM under Flex and counts against that quota.

Observed on Software/Flex only; please route to the Software/Flex component if this belongs elsewhere.

**To Reproduce** (one large run; no smaller reproduction yet)

1. Create a dedicated Flex database: one primary shard, Search on Flash, Flex RAM 4 GiB, logical size 1 TiB. Search disk limits: 24% maximum, 6% minimum, 3 MiB per index.
2. Write about 100M HASHes under one prefix, 105 fields each (the ClickBench `hits` table).
3. Create an index on the prefix with `FT.CREATE … ON HASH … SKIPINITIALSCAN`. Use 77 `NUMERIC` and 28 `TAG SEPARATOR "\x1f" CASESENSITIVE` attributes, with no `SORTABLE` and no `INDEXEMPTY`.
4. Feed the existing documents to the index. Per our client code, this rewrites one field of each HASH with its existing value, in batches of up to 256 keys; this run did not log it separately.
5. Sample `FT.INFO` as documents are indexed. At 30,972,416 indexed documents, the next command (a small read-only Redis Function call) failed with the error above.

**Expected behavior**

The per-document metadata fits the configured Search on Flash budget. If it doesn't, the limit is documented, and the failure says which resource ran out and how far indexing got.

**Questions**

- Which per-document and per-key metadata stays in RAM under Flex in this version, and does it count against the Flex RAM quota?
- Is `doc_table_size_mb` a usable proxy for non-evictable RAM? Could quota-charged memory be reported separately from flash and OS memory?
- Do the Search disk percentages bound all native Search memory? If not, could the docs say so?
- Which version, setting or offload/compaction improvement reduces this footprint, and what per-document sizing should we plan for?

**Environment**

- Redis Software 8.2.6-58, Redis 8.8.0, Search 8.8.8; Flex with Search on Flash; query performance factor 4x.
- No cgroup OOM or OOM-kill events. Server working set was about 8.2 GiB.

**Additional context**

We have not proven that every reported DocTable byte is resident, or identified which allocation is charged to the quota. A larger RAM allocation is a candidate workaround, not a demonstrated fix. Related but distinct: #12458 (TAG membership after GC) and #12328 (scoped backfill with a completion fence).
