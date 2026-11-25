Target: apache/arrow-nanoarrow — new issue (no issue template; use the "[R]" title prefix seen on existing R issues)
Title: [R] Timestamp → POSIXct casts int64 ticks to double before scaling; every current nanosecond timestamp triggers a lossy-conversion warning
Status: R reproduction NOT executed (nanoarrow is not installed here); the expected output comes from reading the 0.9.0 source.
---
**Description**

For TIMESTAMP input, `nanoarrow_materialize_posixct()` first calls `nanoarrow_materialize_dbl()`, converting the raw int64 ticks to double ([materialize_posixct.h#L40-L43](https://github.com/apache/arrow-nanoarrow/blob/apache-arrow-nanoarrow-0.9.0/r/src/materialize_posixct.h#L40-L43)). It then multiplies by 1e-3, 1e-6 or 1e-9 ([L48-L71](https://github.com/apache/arrow-nanoarrow/blob/apache-arrow-nanoarrow-0.9.0/r/src/materialize_posixct.h#L48-L71)).

`materialize_dbl` counts every int64 outside ±2^53 as lossy ([materialize_dbl.h#L91-L101](https://github.com/apache/arrow-nanoarrow/blob/apache-arrow-nanoarrow-0.9.0/r/src/materialize_dbl.h#L91-L101)) and warns ([L154-L157](https://github.com/apache/arrow-nanoarrow/blob/apache-arrow-nanoarrow-0.9.0/r/src/materialize_dbl.h#L154-L157)). Present-day nanosecond timestamps are about 1.7e18 ticks, far above 2^53 (about 9.0e15). So converting any of them warns, even a whole second whose POSIXct value is exact. Casting before scaling also rounds twice; we have not measured whether the final POSIXct ever differs from a correctly rounded result.

Versions: nanoarrow 0.9.0 (CRAN); R 4.6.2; macOS arm64. The code is unchanged on main (the file was last modified in `e134b4a`, 2023-01-03).

**Reproduction (NOT EXECUTED; expected output from reading the source)**

```r
library(nanoarrow)
ticks <- bit64::as.integer64(c("1700000000000000000", "1700000000123456789"))
arr <- as_nanoarrow_array(ticks, schema = na_timestamp("ns", timezone = "UTC"))
x <- convert_array(arr)
#> Warning: 2 value(s) may have incurred loss of precision in conversion to double()
unclass(x)[1] == 1700000000
sprintf("%.7f", unclass(x))
```

**Expected:** no warning when the result is as precise as a POSIXct double allows. The first value is exactly 1700000000.

**Actual (from source):** a `nanoarrow_warning_lossy_conversion` for both values.

**Suggested fix**

For int64 TIMESTAMP storage with sub-second units, split the ticks in integer arithmetic: `(double)(ticks / factor) + (double)(ticks % factor) / factor`. Count a value as lossy only when the whole seconds exceed ±2^53. We apply exactly this as a local source patch, validated only in our own test environment: [install_acceptance.R#L8-L42](https://github.com/redis/redis-adbc-driver/blob/682fae1f73419b3efb4d5e011b9da0a4ca1d5588/r/install_acceptance.R#L8-L42). We're happy to turn it into a PR with tests if the approach is acceptable.
