Target: RediSearch/RediSearch — new issue (bug_report.md template: "[BUG]" prefix, label `bug`). Alternatively the internal Redis Software/Flex channel.
Title: [BUG] Search on Flash: TAG compaction fails with "operand's last doc id is not greater than previous operands'" and the shard aborts (SIGABRT) while indexing ~100M HASHes
Status: facts match #574 and benchmarks/clickbench/managed-ingestion-2026-11-20/issue-566.md:79-130. Before posting: record the resulting URL in #574.
---
**Describe the bug**

On Redis Software Flex with Search on Flash, building a new index over about 100M existing HASHes crashed the shard. The Search log showed an inverted-index merge-order error:

```
operand's last doc id is not greater than previous operands'
```

A failed TAG column-family compaction and a Search panic followed. Redis exited with SIGABRT (signal 6) and restarted inside the same container. There were no OOM or OOM-kill events.

This is filed separately from #12458, which mentions this abort only as possibly related. A shard abort is a different symptom from wrong TAG membership, and no shared cause is established.

**To Reproduce** (one full-scale run; no minimal reproduction yet)

1. Dedicated Flex database: one shard, Search on Flash, 12 GiB Flex RAM cache, 24 GiB container memory limit, persistence off.
2. One client writes 99,997,497 HASHes under one prefix (105 fields, the ClickBench `hits` table), taking about 3 h 40 min.
3. Create `FT.CREATE … ON HASH … SKIPINITIALSCAN` with 77 `NUMERIC` and 28 `TAG SEPARATOR "\x1f" CASESENSITIVE INDEXEMPTY` attributes, with no `SORTABLE`.
4. Feed existing HASHes to the index by rewriting one field with its existing value, in batches of up to 256 keys via a Redis Function. This comes from our client code at the commit used; the run did not log it separately.
5. Indexing ran at about 3,770 documents/s. The last sample before the abort showed 1,584,640 indexed documents and zero indexing failures.

After restart, the database was empty because persistence was off, so the index state could not be inspected.

**Expected behavior**

Compaction keeps doc IDs ordered, or it reports an error without aborting the shard.

**Environment**

- Redis Software 8.2.6-58, Redis 8.8.0, Search 8.8.8; Flex (bigstore v2), Search on Flash; query performance factor 4x.
- Linux ARM64 Docker container. Flash data was on the container's writable overlayfs layer, not a dedicated NVMe device.
- Before the crash: about 14.7 GiB working set and 19.0 GiB raw of the 24 GiB limit, with about 1,179 GiB of flash free.

**Additional context**

We don't know whether `INDEXEMPTY`, overlayfs or scale is needed to trigger this. A later 2,013,006-row load without `INDEXEMPTY` completed, but it was not a controlled contrast. We can share sanitized log excerpts with maintainers on request.

- Is this a known issue, and is it fixed in a later Software/Search build?
- What diagnostics should we collect if we run it again?

Public measurement record: https://github.com/redis/redis-adbc-driver/blob/682fae1f73419b3efb4d5e011b9da0a4ca1d5588/benchmarks/clickbench/managed-ingestion-2026-11-20/issue-566.md#L79-L130
