Target: redis/redis-adbc-driver — new issue
Title: PostgreSQL 18: SQL cursors (DECLARE/FETCH/CLOSE) and CREATE VIEW … WITH CHECK OPTION have no tracking issue
Labels: enhancement, needs-triage
---
## Problem

`docs/postgresql18-compatibility.md` lists two gaps that no issue owns:

- `:54`: statement dispatch "lacks DECLARE/OPEN/CLOSE cursor statements; Search cursors and Arrow streaming are not SQL cursor semantics". Only row locking has a tracker (#116).
- `:49`: "CREATE VIEW parsing does not implement WITH CHECK OPTION".

The umbrella tracker #118 is closed, and so are the corpus issues #359 and #361. A search of non-test `go/` sources finds no cursor-statement or `CHECK OPTION` handling. The `go/pgdiff` corpus has no case for either feature. So the corpus pass rate (602/651, `docs/sdlc/changes/postgresql18-final-query-gaps/validation.md:14-16`) says nothing about them.

Not verified: what the driver returns today for the following:
- `DECLARE c CURSOR FOR …`
- `FETCH`
- `CLOSE`
- `CREATE VIEW … WITH [LOCAL | CASCADED] CHECK OPTION`

Expected: either PostgreSQL-compatible behavior, or a clear feature-not-supported error (SQLSTATE `0A000`). A generic syntax error or a silently ignored clause would not meet that.

## Desired behavior

Decide the scope for each feature:

- **Cursors:** support them within the driver's autocommit-only model ("All modes use autocommit", `README.md:100`), or reject them with `0A000` and document why.
- **WITH CHECK OPTION:** enforce the view predicate on writes through updatable views, if the driver supports those, or reject the clause explicitly instead of ignoring it.

## How to verify

- Add `go/pgdiff` corpus cases for both features and compare them against the pinned PostgreSQL 18 reference with `make -C go pgdiff`. This needs `REDIS_URI`; `make -C go redis-up` provides one.
- Run `make -C go pgdiff-test` for the harness checks.
- Update the rows at `docs/postgresql18-compatibility.md:49` and `:54` to match the outcome.
