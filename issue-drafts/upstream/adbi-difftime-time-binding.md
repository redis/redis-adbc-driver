Target: r-dbi/adbi — new issue (plain .github/ISSUE_TEMPLATE.md: brief description + reprex)
Title: dbBind() sends difftime parameters as Arrow duration even when the prepared parameter schema asks for time32/time64
Status: R reproduction NOT executed (adbi and nanoarrow are not installed here); the expected output comes from reading the source at the linked tags.
---
**Brief description of the problem**

A prepared statement's ADBC parameter schema can declare a TIME placeholder (`time32`/`time64`). Even then, `dbBind()` with an R `difftime` sends an Arrow `duration[us]` column.

`dbBind_AdbiResult()` reads the parameter schema ([L36-L40](https://github.com/r-dbi/adbi/blob/ac5d3294597c453c8cb1c278b9536cd9afb3eae2/R/dbBind_AdbiResult.R#L36-L40)). It then calls `adbc_statement_bind_stream(res@statement, params)` without that schema ([L83](https://github.com/r-dbi/adbi/blob/ac5d3294597c453c8cb1c278b9536cd9afb3eae2/R/dbBind_AdbiResult.R#L83)). nanoarrow infers `difftime` as `duration[us]` ([schema.R#L125-L129](https://github.com/apache/arrow-nanoarrow/blob/apache-arrow-nanoarrow-0.9.0/r/R/schema.R#L125-L129)). A driver following PostgreSQL semantics reads `duration` as INTERVAL, so the TIME placeholder gets an interval, not a time of day.

Versions: adbi 0.1.4 at `ac5d329` (current main); the same call is in 0.1.3 ([L85](https://github.com/r-dbi/adbi/blob/v0.1.3/R/dbBind_AdbiResult.R#L85)). adbcdrivermanager 0.25.0, nanoarrow 0.9.0, R 4.6.2.

```r
# NOT EXECUTED: drafted without adbi/nanoarrow installed.
library(nanoarrow)
x <- as.difftime(3723.5, units = "secs")   # 01:02:03.5

# What dbBind() currently hands to adbc_statement_bind_stream():
infer_nanoarrow_schema(data.frame(t = x))$children$t$format
#> expected from source: "tDu" (duration, microseconds)

# What a time64[us] parameter schema asks for; nanoarrow already converts it:
convert_array(as_nanoarrow_array(x, schema = na_time64("us")))
#> expected: 01:02:03.5
```

End to end: take any driver whose `AdbcStatementGetParameterSchema` reports `time64` for a placeholder. `dbBind(res, list(as.difftime(3723.5, units = "secs")))` should bind a TIME. Today it sends `duration[us]`. We hit this with the Redis ADBC driver, which reports `time64[us]` for TIME parameters and maps `duration` to INTERVAL. We kept no public log of the unpatched failure, so we can't quote an error here.

**Possible fixes (not tested upstream)**

1. When the parameter schema is known, pass it through: `adbc_statement_bind_stream(res@statement, params, schema = …)`. adbcdrivermanager 0.25.0 accepts `schema` ([adbc.R#L561-L562](https://github.com/apache/arrow-adbc/blob/apache-arrow-adbc-25/r/adbcdrivermanager/R/adbc.R#L561-L562)). We have not checked data-frame-to-struct conversion when child names are empty or `$n`.
2. A narrower fix, which we apply locally: reclass `difftime` columns as `hms` when their schema child is `time32`/`time64`. This inherits nanoarrow's `hms` default of `time32[ms]`. See [install_acceptance.R#L63-L73](https://github.com/redis/redis-adbc-driver/blob/682fae1f73419b3efb4d5e011b9da0a4ca1d5588/r/install_acceptance.R#L63-L73).

Related: #127 (empty positional parameter names), in the same function.
