# dbBind rejects ADBC schemas with empty positional parameter names

Submitted 2026-10-30: [upstream report](https://github.com/r-dbi/adbi/issues/127).
Fix PR: [upstream pull request](https://github.com/r-dbi/adbi/pull/128).

ADBC requires empty field names for unnamed parameters. adbi 0.1.3 and upstream
`ac5d3294597c453c8cb1c278b9536cd9afb3eae2` (version 0.1.4 in DESCRIPTION)
treat these as named placeholders, rejecting `dbBind(res, list(2L))` with
`Expecting named params for named placeholders`.

Reproduce against the native Redis ADBC driver:

```r
library(DBI)
library(adbi)
driver <- adbcdrivermanager::adbc_driver(Sys.getenv("REDIS_ADBC_LIBRARY"))
con <- dbConnect(adbi(driver), uri = Sys.getenv("REDIS_URI"))
res <- dbSendQuery(con, "SELECT CAST(? AS INTEGER) AS value")
schema <- nanoarrow::nanoarrow_schema_parse(
  adbcdrivermanager::adbc_statement_get_parameter_schema(res@statement), recursive = TRUE)
stopifnot(identical(names(schema$children), ""))
dbBind(res, list(2L))
stopifnot(dbFetch(res)$value == 2L)
dbClearResult(res)
dbDisconnect(con)
```

The positional metadata is correct under the
[ADBC contract](https://arrow.apache.org/adbc/20/cpp/api/group__adbc-statement.html).
`named_schema` compares names only to numeric and `$n` conventions; an empty name
passes both inequalities. Adding `nzchar(names(schema))` fixes this classification.
`dbBindArrow()` delegates to the same check in the installed package, so it does
not provide a workaround.

The accompanying [patch](adbi-empty-parameter-names.patch) includes a test using
unnamed metadata with SQLite and preserves parameter count/named-input checks.
Local Redis integration passed single/multiple positional values, NULLs, mixed
NULL/integer values and rebinding with the patched upstream package. The same
four cases fail with unpatched upstream. The driver schema was unchanged.

Environment: macOS arm64, R 4.6.2, DBI 1.3.0, adbcdrivermanager 0.25.0,
nanoarrow 0.9.0; Redis Software 8.6.2, Search, verified active 4x QPF.
Local tracker: https://github.com/redis/redis-adbc-driver/issues/252.

Prepared with AI assistance (OpenAI Codex).
