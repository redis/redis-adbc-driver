# Redis DBI adapter

`redis_adbi.R` specializes upstream **adbi** for Redis's SQL types and catalog
visibility. It supplies BOOLEAN, BYTEA, TIME, BIGINT and timezone-aware timestamp
mappings, typed/append/overwrite writes, quoted identifiers, and session-local
temporary table handling. It loads the native library directly.

From a repository checkout, with a dedicated test R library:

```sh
export R_LIBS_USER=/tmp/redis-adbc-r-library
Rscript r/install_acceptance.R
```

The installer builds nanoarrow **0.9.0** and adbi **0.1.4** at upstream commit
`ac5d3294597c453c8cb1c278b9536cd9afb3eae2`, plus small source fixes included in
the installation recipe. It requires a C/C++ compiler and downloads public R
dependencies. It does not replace packages in the default user library.

The adbi fixes recognize empty positional parameter names in ADBC schemas and
bind R `difftime` values as TIME when the prepared parameter schema requests it.
The nanoarrow fix converts timestamp ticks into whole/fractional seconds before
converting to POSIXct doubles, avoiding precision loss and warnings caused by
converting large raw tick counts first. These are **patched dependencies**, not
claims about the unmodified CRAN releases. Their source changes are explicit in
`install_acceptance.R`; DBItest expectations are unchanged.

```r
source("r/redis_adbi.R")
con <- DBI::dbConnect(redis_adbi(), uri = Sys.getenv("REDIS_URI"), bigint = "integer64")
DBI::dbWriteTable(con, "example", data.frame(id = 1L, active = TRUE))
stopifnot(identical(DBI::dbReadTable(con, "example")$active, TRUE))
DBI::dbRemoveTable(con, "example")
DBI::dbDisconnect(con)
```

Set `REDIS_ADBC_LIBRARY` to an absolute native-library path and use verified TLS
with an appropriate CA. The acceptance runner separates the private Redis CA
from package-download trust. Writes autocommit: transactions, rollback and
uniqueness enforcement remain unsupported. Temporary tables belong to one
connection. Schema-qualified SQL identifiers are quoted through DBI.
