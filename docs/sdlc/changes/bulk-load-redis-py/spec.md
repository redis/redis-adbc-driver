# Bulk-loading measurement contract

2026-10-22. See [intent](intent.md) and [plan](plan.md).

- Official 100K-row/105-column ClickBench prefix, independently typed into Arrow.
- Current source-identified ADBC build versus redis-py 8.1.0/hiredis, RESP3.
- One or four fresh writer processes, 10,000-row Arrow batches and at most
  1,000 HSET commands per pipeline for both clients.
- Both use identical common empty-table/index setup and write the same HASH
  field encodings plus allocated row IDs. Native writes omit generic SQL
  constraints, metadata fencing and concurrent-DDL semantics.
- Include common setup, worker startup, Arrow IPC input reads, conversion,
  connections, all writes, process teardown and index readiness. Report last
  write acknowledgement separately. Exclude source preparation, cleanup and
  independent full-value/multiplicity validation.
- Dedicated local Redis 8.6.2 container, four CPUs/two GiB, no persistence/TLS.
  Clear only that owned database between trials. Record shared-host limits.
- Four smoke cases; one warmup and six alternating measured rounds per case.
  Preserve failures separately and require exact validation of all final runs.
- Preserve the deck's design; place one bulk-load slide before the read
  comparisons, retain native editability and document timing/semantic limits.
