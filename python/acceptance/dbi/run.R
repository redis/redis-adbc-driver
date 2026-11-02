args <- commandArgs(trailingOnly = TRUE)
if (nzchar(Sys.getenv("REDIS_ACCEPTANCE_CA_FILE"))) {
  Sys.setenv(SSL_CERT_FILE = Sys.getenv("REDIS_ACCEPTANCE_CA_FILE"))
}
for (pkg in c("DBI", "adbi", "adbcdrivermanager", "DBItest", "testthat", "xml2")) {
  if (!requireNamespace(pkg, quietly = TRUE)) stop(paste("Install R package", pkg))
}
reporter <- testthat::JunitReporter$new(file = args[[2]])
source(file.path(Sys.getenv("REDIS_ADBC_REPO"), "r", "redis_adbi.R"))
setup <- DBI::dbConnect(redis_adbi(), uri = Sys.getenv("REDIS_URI"))
schema <- paste0("it_acceptance_dbi_", Sys.getpid(), "_", sample.int(1e9, 1))
Sys.setenv(REDIS_ACCEPTANCE_SCHEMA = schema)
DBI::dbExecute(setup, paste("CREATE SCHEMA", DBI::dbQuoteIdentifier(setup, schema)))
tryCatch(
  testthat::test_file(args[[1]], reporter = reporter, stop_on_failure = TRUE, stop_on_warning = FALSE),
  finally = {
    DBI::dbExecute(setup, paste("DROP SCHEMA", DBI::dbQuoteIdentifier(setup, schema), "CASCADE"))
    DBI::dbDisconnect(setup)
  }
)
