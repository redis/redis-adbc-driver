# Specification: SQL and Arrow operation deadlines

Date: 2026-11-20. Source: `c6a75ca2`. Status: implemented and locally verified.
Requestor: Albert Wong. Author: Codex. Issue: [#546](https://github.com/redis/redis-adbc-driver/issues/546).
Related: [intent](intent.md), [plan and evidence](plan.md).

## Contract

- Native string option `adbc.redis.statement_timeout` supports database,
  connection and statement scope. Accept nonnegative durations or seconds;
  zero means unlimited. Preserve the existing 15-minute default.
- Highest precedence: explicit statement option, SQL session/local value,
  connection default inherited at open from the database, built-in default.
- Setting the connection option updates its reset default and clears SQL
  overrides. RESET/RESET ALL restore that default. Database changes affect
  subsequent opens; existing sessions are independent.
- SQL timeout syntax retains milliseconds when unitless and its existing
  PostgreSQL duration units/range. SHOW and pg_settings report session policy;
  statement GetOption reports its effective override/session duration.
- Each SQL statement through ExecuteQuery/ExecuteUpdate gets its own deadline, including streamed results;
  a script's SET changes affect subsequent statements. An Arrow call has one
  enclosing deadline across admission, all producer batches and publication.
  Arrow Next is a caller callback and cannot be forcibly interrupted.
- Apply the policy to managed, unmanaged and live SQL/Arrow paths, including
  source creation/replacement. Nested metadata registration/import inherits the
  enclosing policy instead of imposing another fixed 15-minute ceiling.
- Earlier parent deadlines/cancellation remain effective. Zero removes only the
  driver deadline. Socket timeouts, renewable leases, fencing, bounded writes,
  independent cleanup and non-replay rules are unchanged.
- Managed failure publishes no partial generation. Application-owned writes
  retain their acknowledged-row accounting and partial-write semantics.

## Implementation boundaries and risks

Resolve policy at execution time, so existing statements see later session
changes unless explicitly overridden. Carry a resolved timeout in execution
context through nested metadata/import work. Keep stream pin/cancel ownership
on existing readers and cleanup paths. Keep maintenance/retention defaults
separate. Unlimited operations can live until caller cancellation or close;
this is explicit opt-in and does not make leases unlimited.

## Measurable checks

Option/default/session reset tests; caller cancellation/deadline tests; delayed
SQL command and Arrow producer tests under short/long/unlimited policy; managed
publication/old-reader/pin checks; both application ownership paths; existing
session, ingestion, cancellation, cleanup and stream regressions. Use Redis
Software with verified active 4x QPF, separately from the unit-only pass.
