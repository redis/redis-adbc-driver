# Contract

Date: 2026-10-21. Base: `cebe8088`. Implemented in `85330bd6`; locally verified.
See [intent](intent.md) and [plan](plan.md).

Only #404's guarded simple managed-table projection is eligible. Keep one
reusable scratch row to preserve column validation/fitting order; append its
values directly to Arrow builders rather than accumulating projected rows.
Retain at most a partial Arrow batch plus complete batches from one cursor-page
overshoot. Public batches never exceed stream_batch_rows. Do not borrow raw
Redis buffers. Preserve defaults/null markers, aliases, duplicate columns,
precision, required unselected-field validation, sparse ranges and cancellation.

Small ordinary results remain detached, with a known row count, after final
metadata checks. Live streams retain ownership and cursor cleanup. Early release,
cancel and partial-row errors release partial builders and queued records;
records explicitly retained by a consumer remain valid.

Compare current-main and candidate release builds on the unchanged wide/narrow
fixture through both public APIs. Preserve exact values/multiplicity and publish
allocation, latency, CPU, memory and transport evidence. Include current official
redis-py references with both known keys and Search discovery. No production or
100-million-row transfer inference.
