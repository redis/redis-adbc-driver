Target: redis/redis-adbc-driver — comment on #577
---
Upstream report posted 2026-11-24: https://github.com/RediSearch/RediSearch/issues/12531

Before posting, I searched RediSearch/RediSearch for "DocTable", "bigredis-max-ram" and "doc_table_size_mb flash" and found no equivalent report. The nearest issue, RediSearch/RediSearch#12458 (INDEXEMPTY membership after GC), is a different problem.

The report keeps only the observations recorded here:
- versions;
- the Flex and Search-on-Flash limits;
- the stop at 30,972,416 documents;
- the OOM message;
- DocTable ≈4044 MiB with KeyTable 0, and no cgroup OOM.

It asks the four questions above, and says that the resident-versus-reported DocTable bytes and the quota-charged allocation are unproven.

The full-load TAG compaction abort from #574 is reported separately as https://github.com/RediSearch/RediSearch/issues/12532, not folded into RediSearch/RediSearch#12458.

Keep this issue open until upstream responds with guidance or a fixed version.
