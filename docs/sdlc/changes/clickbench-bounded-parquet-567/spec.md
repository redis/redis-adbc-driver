# Bounded Parquet stream ownership: specification

Date: 2026-11-20. Source: 1b5918e6. Status: implemented; validation limits recorded.
Issue: https://github.com/redis/redis-adbc-driver/issues/567

## Contract and boundaries

In managed-streams mode, one current row-group reader per worker, pre_buffer=False, no memory mapping, no producer queue and deterministic close. Reuse only immutable metadata. Preserve Arrow buffer lifetime after reader closure, exact int64/date/time/string values, uneven groups and partial batches. No allocator purges or hard RSS-bound claim.

See [intent](intent.md) and [plan/evidence](plan.md).
