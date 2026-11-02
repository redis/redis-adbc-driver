args <- commandArgs(trailingOnly = TRUE)
if (nzchar(Sys.getenv("REDIS_ACCEPTANCE_CA_FILE"))) {
  Sys.setenv(SSL_CERT_FILE = Sys.getenv("REDIS_ACCEPTANCE_CA_FILE"))
}
for (pkg in c("DBI", "adbi", "adbcdrivermanager", "DBItest", "testthat", "xml2")) {
  if (!requireNamespace(pkg, quietly = TRUE)) stop(paste("Install R package", pkg))
}
reporter <- testthat::JunitReporter$new(file = args[[2]])
testthat::test_file(args[[1]], reporter = reporter, stop_on_failure = TRUE,
                    stop_on_warning = FALSE)
