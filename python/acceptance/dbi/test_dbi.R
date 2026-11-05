# Exercise the upstream DBI contract, including its unsupported-feature checks.
# Unsupported transaction/package contracts are declared explicitly below.
source(file.path(Sys.getenv("REDIS_ADBC_REPO"), "r", "redis_adbi.R"))
options(adbi.allow_multiple_results = FALSE, adbi.force_close_results = TRUE)
DBItest::make_context(
  redis_adbi(),
  connect_args = list(uri = Sys.getenv("REDIS_URI"), bigint = "integer64",
                      adbc.redis.default_schema = Sys.getenv("REDIS_ACCEPTANCE_SCHEMA")),
  tweaks = DBItest::tweaks(
    dbitest_version = "1.8.3",
    constructor_name = "redis_adbi",
    constructor_relax_args = TRUE,
    placeholder_pattern = c("?", "$1"),
    date_cast = function(x) paste0("DATE '", x, "'"),
    time_cast = function(x) paste0("TIME '", x, "'"),
    timestamp_cast = function(x) paste0("TIMESTAMP '", x, "'")
  ),
  name = "Redis ADBC"
)
# The source helper is not an R package, and this driver explicitly rejects
# transactions. Test autocommit rejection separately; do not emulate rollback.
DBItest::test_all(skip = c("package_name", "begin_.*", "commit_.*", "rollback_.*", "with_transaction_.*"))

testthat::test_that("Redis DBI rejects transactions and keeps autocommitted writes", {
  con <- DBI::dbConnect(redis_adbi(), uri = Sys.getenv("REDIS_URI"), bigint = "integer64")
  table <- paste0("it_dbi_autocommit_", Sys.getpid())
  tryCatch({
    testthat::expect_error(DBI::dbBegin(con), "NOT_IMPLEMENTED|not implemented|autocommit")
    DBI::dbWriteTable(con, table, data.frame(id = 1L))
    testthat::expect_error(DBI::dbRollback(con), "NOT_IMPLEMENTED|not implemented|autocommit|without transactional context")
    testthat::expect_identical(DBI::dbReadTable(con, table)$id, 1L)
  }, finally = {
    if (DBI::dbExistsTable(con, table)) DBI::dbRemoveTable(con, table)
    DBI::dbDisconnect(con)
  })
})
