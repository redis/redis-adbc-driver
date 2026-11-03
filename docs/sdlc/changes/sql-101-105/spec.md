# Specification

2026-11-03. Base `bb83527`. Status: implementation in review.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

The public contract and limitations are in [the SQL addition guide](../../../sql-101-105.md).

- Sequences are exact int64 allocators in the metadata slot, with connection-local
  cached blocks and currval/lastval state. SERIAL and identity defaults use them.
- UUIDs are canonical text, indexed as TAG, with SQL type metadata on Arrow utf8.
- Top-level modifying CTEs run once. Physical input images are captured before
  own writes; previous modifications can remain after a later failure.
- SQL index names belong to a schema and are committed with metadata. Search
  fields are added, and remain after dropping the SQL name. UNIQUE is rejected.
- Materialized views store SQL plus a physical generation. Refresh builds before
  publication; reads of an unpopulated generation and writes fail explicitly.

Tests must cover repeated allocation, bounds, caching, distinct connections,
identity overrides and ALTER, omitted ingest columns, ownership and drop,
canonical UUIDs and Arrow metadata, modifying CTE reuse/unused execution and
input images, index metadata and filtering, failed refresh preservation, and
materialized-view rename/drop/write refusal. Use Redis Software with verified
4x QPF and full result consumption. Review sequence ownership, metadata-slot
transactions, physical index publication and adopted HASH restrictions.
