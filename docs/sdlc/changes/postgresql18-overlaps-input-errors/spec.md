# PostgreSQL 18 overlaps and input errors: spec

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `61043e1a`. Status: implemented; local validation complete; pending PR review.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

OVERLAPS accepts two pairs of temporal endpoints or supported start/interval overloads. Normalize reversed bounds, use half-open intervals with singleton exceptions, preserve three-valued partial-NULL behavior, session timezone/DST arithmetic and exact Arrow time precision. Resolve incompatible and ambiguous signatures before scanning, including empty results. The named overlaps() function is outside this operator-only batch.

pg_input_error_info(text,text) is a scalar record with nullable text OUT fields message, detail, hint and sql_error_code. FROM exposes exactly one row; aliases, LATERAL, ROWS FROM and ordinality use existing table-function paths. Valid input returns four NULL fields; NULL arguments produce a NULL scalar record. Unknown types raise 42704. Conversion errors are returned as data, preserving SQLSTATE and separating supported details from the primary message.

Input diagnostics cover booleans, integers, floats, character strings, bounded numerics and integer arrays using existing exact parsers. Other diagnostic target types and unconstrained numerics remain explicitly unsupported (0A000); do not return a false PostgreSQL rejection for storage representation limits. Numeric special values remain outside this batch. Existing pg_input_is_valid scope is unchanged. Array shape and other existing parser limits still apply; this batch does not claim full PostgreSQL input-function coverage.

Primary references: [PostgreSQL 18 date/time operators](https://www.postgresql.org/docs/18/functions-datetime.html), [input validity functions](https://www.postgresql.org/docs/18/functions-info.html#FUNCTIONS-INFO-DATA-VALIDITY). The pinned PostgreSQL oracle determines retained edge-case expectations.
