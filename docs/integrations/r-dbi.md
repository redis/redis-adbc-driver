# Redis integration guide for R / DBI

Complete the [shared setup](example-setup.md) first, including the native driver
path and `REDIS_URI`. Install R and these packages in the R environment you use:

```r
install.packages(c("DBI", "adbi", "adbcdrivermanager"), repos="https://cloud.r-project.org")
```

## Query from R

Save the R block as `redis-guide.R` and run `Rscript redis-guide.R`. No Python helper is needed by the R process.
Keep `REDIS_ADBC_TEST_TABLE` unset so the harness creates and drops its own table.

```r
# Run with Rscript or source() in an RStudio R session.
# Requires DBI, adbi, adbcdrivermanager, REDIS_URI and REDIS_ADBC_LIBRARY.
redis_adbc_r_smoke <- function() {
  library(DBI)
  library(adbi)
  driver <- adbcdrivermanager::adbc_driver(Sys.getenv("REDIS_ADBC_LIBRARY"))
  con <- dbConnect(adbi(driver), uri = Sys.getenv("REDIS_URI"))
  name <- Sys.getenv("REDIS_ADBC_TEST_TABLE")
  created <- !nzchar(name)
  if (created) name <- paste0("it_r_session_", Sys.getpid())
  output <- paste0("guide_r_output_", Sys.getpid(), "_", sample.int(1e9, 1))
  on.exit({
    dbExecute(con, sprintf('DROP TABLE IF EXISTS "%s"', output))
    if (created) dbExecute(con, sprintf('DROP TABLE IF EXISTS "%s"', name))
    dbDisconnect(con)
  }, add = TRUE)
  if (created) {
    dbExecute(con, sprintf('CREATE TABLE "%s" (id BIGINT, label TEXT, amount DOUBLE, active BOOLEAN)', name))
    dbExecute(con, sprintf("INSERT INTO \"%s\" VALUES (1, 'alpha', 2.5, TRUE), (2, NULL, NULL, NULL), (3, '雪', 7.5, FALSE)", name))
  }
  data <- dbGetQuery(con, sprintf('SELECT * FROM "%s" ORDER BY id', name))
  stopifnot(identical(as.numeric(data$id), c(1, 2, 3)))
  stopifnot(identical(data$label, c("alpha", NA_character_, "雪")))
  stopifnot(identical(data$active, c(TRUE, NA, FALSE)))
  stopifnot(identical(data$amount, c(2.5, NA_real_, 7.5)))
  empty <- dbGetQuery(con, sprintf('SELECT * FROM "%s" WHERE FALSE', name))
  stopifnot(nrow(empty) == 0L, identical(names(empty), names(data)))
  # Persist an R-computed summary, rather than counting fixture setup as a write.
  dbExecute(con, sprintf('CREATE TABLE "%s" (total DOUBLE)', output))
  total <- sum(data$amount, na.rm = TRUE)
  dbExecute(con, sprintf('INSERT INTO "%s" VALUES (%f)', output, total))
  stored <- dbGetQuery(con, sprintf('SELECT total FROM "%s"', output))
  stopifnot(identical(stored$total, total))
  dbExecute(con, sprintf('DROP TABLE "%s"', output))
  cat("REDIS_ADBC_READ_WRITE_OK\n")
  invisible(data)
}
redis_adbc_r_smoke()
report <- Sys.getenv("REDIS_ADBC_R_REPORT")
if (nzchar(report)) writeLines("REDIS_ADBC_R_DBI_OK", report)
cat("REDIS_ADBC_R_DBI_OK\n")
```

## Expected result and limits

The script checks typed values, NULLs, Unicode and the empty-result schema,
cleans up its table, and prints `REDIS_ADBC_R_DBI_OK`.
R's driver manager loads the absolute native library path; installing a Python
package alone does not register an R backend. Writes autocommit.

The recorded adbi 0.1.3 release rejects some positional binding schemas. This
read example avoids binding; do not infer that `dbBind()` works from its success.
See the [binding follow-up](../sdlc/changes/integration-252-254/validation.md) and
[guide test results](../sdlc/changes/integration-guides-262/validation.md). The
RStudio guide's console path and broader IDE interactions are distinct checks.

From the repository root, rerun the guide's query block (not the package-install
block) with the same R environment:

```sh
python python/scripts/check_integration_guide.py r-dbi
```

[All integration guides](README.md)
