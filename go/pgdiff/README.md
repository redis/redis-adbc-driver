# Postgres differential corpus

From `go/`, against an already running test Redis:

```sh
REDIS_URI=redis://localhost:6379/0 make pgdiff
```

Requires Docker and uv. The target builds the current driver, starts an ephemeral
Postgres **16.15**, pinned by its linux/amd64 manifest digest, and removes that
container on exit. It initializes UTF-8 with `--locale=C`; both sessions use UTC,
ISO dates. Postgres uses ISO interval text for lossless decoding; Redis returns
Arrow intervals directly. Redis gets fresh `pgdiff_<uuid>` schemas, dropped
after each file. Use a test database: connections can recover abandoned driver
operations. No database-wide flush is used.

The runner prints pass rates by file and area and every unexpected difference.
`go/build/pgdiff-report.json` contains SQL, both outcomes, raw Postgres OIDs and
Arrow types, source issue references, and reference version/collation. Differences
or blocked cases exit with status 1; infrastructure errors also exit nonzero.
Setup statements don't inflate the pass rate; failed setup is reported and blocks
that file. Allowed and unsupported cases are counted separately and excluded from
the denominator (`pass / (pass + fail)`).

## Corpus format

Each JSON file declares `area`, optional `source` and `setup` (an array of single
SQL statements), and `cases` with stable `id` and `sql`. Cases run sequentially,
so writes are visible to subsequent cases. An expected SQL error doesn't abort
the file. Every file gets its own schema on both engines. Keep independent
mutation scenarios in separate files. No SQL script splitting is needed.

Rows compare as multisets, preserving duplicates, unless the statement has an
outer `ORDER BY`. Subquery/window/aggregate ordering doesn't order the result.
`ordered: true/false` overrides detection for unusual SQL. Use a total ordering
when ties matter. Queries without ordering and with LIMIT can pick different
valid subsets; avoid such queries in the corpus.

Types compare through the physical Arrow mapping in `run.py`: integer widths,
float widths, bool, binary, string, date, time, timestamp (with/without zone),
decimal and interval. Raw types are retained for investigation. Text, varchar,
bpchar, JSON and JSONB all map to Arrow string. Numeric precision/scale and
Arrow timestamp units aren't scored yet because Postgres result metadata doesn't
always supply a matching typmod; these are limitations of this first harness.
Floats compare exactly, including signed zero and NaN, with no tolerance.
Decimals retain all digits but ignore trailing fractional zeros. JSON text is
compared as returned, so formatting differences remain visible. SQL errors
compare only SQLSTATE; missing SQLSTATE is a failure.

## Documented exceptions

`allowlist.json` maps `file.json:case-id` to an object with `reason`, `issue`, and
exact normalized `postgres` and `redis` snapshots (either `sqlstate`, or `types`
and `rows`). Only those exact outcomes are allowed; changed outcomes fail, and a
fixed case passes. Don't allowlist unresolved compatibility bugs just to make the
run green. The initial allowlist is empty.

A case may declare `unsupported: {"reason": "...", "sqlstate": "0A000"}` for a
feature explicitly excluded by the driver. It counts as unsupported only if
Postgres succeeds and Redis returns that SQLSTATE. Generic parser and missing
function errors aren't automatically excluded.

## Coverage and follow-up

The initial corpus contains selected directly executable repros from #274's
issues, plus smoke cases for duplicates, empty results, types, SQLSTATEs, CTEs,
set operations, aggregates and date/time values. It is a starter corpus, not all
of #274: bound parameters, multi-connection/transaction repros and metadata
queries need additional fixtures. Next add the remaining issue repros, then
captured dbt-postgres/dbt_utils SQL and supported Postgres regression statements,
in the priority order from #236. CI is optional initially.

```sh
make pgdiff-test
make pgdiff PGDIFF_ARGS='--corpus /path/to/corpus --report build/other-report.json'
```
