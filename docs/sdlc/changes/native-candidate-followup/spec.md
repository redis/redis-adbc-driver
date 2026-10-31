# Additional native candidate specification

## Contract

- Ibis consumes fetched Arrow data with `memtable`; check filters, aggregations,
  NULLs, Unicode, and empty output through a local execution backend.
- MLflow logs metadata for fetched data and retrieves/checks its schema/profile
  and dataset input from a local tracking store.
- Beam opens the native driver inside a Python DoFn executed by a local runner;
  check that data is fetched in a separate worker process and verify full output.
- Mage executes a real loader/transformer pipeline, persists the result, and
  checks the rows and empty schema.
- Kestra executes a Python Script task on a real local server, uses the native
  shared library, and exposes checked downstream outputs.
- Each tool has populated/empty cases. Close native connections before asserting
  downstream results, and remove owned fixture data/application processes.
- Preserve separate dependency stacks instead of overriding upstream caps.

## Acceptance

Run all five tools through saved commands against verified 4x-QPF Redis Software.
Record exact versions, outcomes, runtime prerequisites, and remaining limits.
Check the matrix's native path evidence without altering Flight SQL claims.
Keep the earlier sweep's historical result counts separate from this batch.

## Links

- [Intent](intent.md)
- [Plan and evidence](plan.md)
