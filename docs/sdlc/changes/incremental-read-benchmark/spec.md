# Incremental-read measurement contract

2026-10-22. See [intent](intent.md), [plan](plan.md), and [validation](validation.json).

- Return the same independent 100,000-row, 105-column typed ClickBench prefix
  through every path; validate every value and duplicate multiplicity after
  complete consumption, outside timing.
- Match 512/4,096-row output ceilings and require the first batch to contain
  the requested row count. Preserve 4,096-row native retrieval bounds. First
  batches may have different order and variable-length byte content.
- Use RESP3, one or four fresh worker processes, and eight tasks for four
  readers. Compare ADBC statements, public partitions, explicit SQL ranges,
  native known-key HMGET, and native Search discovery plus HMGET as applicable.
- Use a common timestamp after constructing/normalizing a typed Arrow batch;
  record first IPC flush separately. Include planning, worker startup,
  connections, full reads, IPC and teardown in complete elapsed time.
- Keep one warmup per case separate from six measured rounds, balancing
  forward/reverse order. Preserve all raw measurements, including outliers.
- Pin the driver, fixture, runners, package versions and environment. Report
  the shared-host and fixture limitations, and identify the historical driver
  instead of presenting the results as current-main measurements.

Acceptance: all 98 canonical executions and 14 separate smoke executions
validate exactly. Claims must match the raw medians/ranges. ADBC's explicit
ranges reduce median four-reader elapsed time, but overlapping native timing
ranges do not establish superiority. First-batch results establish no ADBC win.

Risk: `--load` replaces `transferbench.hits`; use a dedicated test server.
The experiment supplies immutable dense ranges externally and does not replace
the public partition API's dynamic completeness/readiness guarantees.
Publication: [PR #451](https://github.com/alberttwong/redis-adbc-driver/pull/451).
