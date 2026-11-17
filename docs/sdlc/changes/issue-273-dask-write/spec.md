# Specification

2026-11-17. Source: `19ebcb46`. Status: implemented and locally verified.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

`write_dataframe` returns a lazy Dask Delayed acknowledged-row total. Each task
converts its pandas partition using an exact explicit Arrow schema and safe
conversion, then owns an ADBC connection/cursor through an append. Empty
partitions also validate the existing target with a zero-row append. Column
names/order are exact and the pandas index is excluded.

The target is caller-owned and precreated. Appends within a graph form a
sequential dependency chain; callers serialize other writers. There is no
create/replace/drop path or retry loop. `allow_duplicate_writes=True` is required
because Dask can recompute after worker loss despite zero ordinary task retries.
Repeated computation can duplicate rows. Counts describe observed successful
tasks, not all side effects from lost/replayed attempts.

A serializable `PartitionWriteError` reports partition index, preceding
acknowledged rows, and whether this partition attempted a write. The chained
error retains native status/details where available. Attempted writes can have
committed after failure; existing rows remain. Ordinary failure prevents later
append tasks from running. This is finite append integration, without rollback,
resume, exactly-once delivery or a new consistency guarantee.

Acceptance: graph construction performs no connections; exact NULL/Unicode and
empty partitions round-trip through two distinct worker processes; successful
counts match rows; native refusal and failed partition stop dependent appends,
preserve committed data, expose counts and close handles. Explicit recompute
coverage demonstrates duplicate behavior rather than claiming idempotency.
