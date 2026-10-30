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
  on.exit({
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
  invisible(data)
}
redis_adbc_r_smoke()
report <- Sys.getenv("REDIS_ADBC_R_REPORT")
if (nzchar(report)) writeLines("REDIS_ADBC_R_DBI_OK", report)
cat("REDIS_ADBC_R_DBI_OK\n")
