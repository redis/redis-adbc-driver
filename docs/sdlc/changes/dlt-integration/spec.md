# Specification

Date: 2026-10-29. Source: `95d6583`. Status: implemented.
[Intent](intent.md), [plan](plan.md), [verification](validation.md).

- `adbc_driver_redis.dlt.redis_adbc` is a dlt custom destination. The `dlt`
  optional extra requires `dlt[parquet]>=1.31,<2`; core imports remain independent.
- Parquet normalization provides Arrow RecordBatches, defaulting to 1,000 rows.
  Each batch uses an owned DB-API connection/cursor and `adbc_ingest` in
  `create_append` mode. Jobs run sequentially within a pipeline.
- Configure URI explicitly or through dlt secrets, SQL schema (`public` by
  default), native database options, index columns and dlt batch size.
  dlt dataset names do not select Redis schemas. Identifiers are quoted.
- Only append is accepted. Reject replace/merge before connecting. Skip dlt's
  internal tables/columns and normalize resource names with snake_case.
- Redis autocommit permits partial writes. Retry after a partial batch or lost
  checkpoint can duplicate rows. Primary-key hints do not imply upsert. Callers
  manage migrations, concurrency between pipelines and local state persistence.

Acceptance checks run real dlt extract/normalize/load pipelines and fully fetch
native driver results: batching, append, multiple tables, Arrow input, values and
NULLs, quoted schema names, configuration, rejected dispositions, incremental
state after reopening and resume after a transient pre-write batch failure.
