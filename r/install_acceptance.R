# Reproducible adbi test environment. Never install into a default user library.
lib <- Sys.getenv("R_LIBS_USER")
if (!nzchar(lib)) stop("Set R_LIBS_USER to a dedicated test library")
dir.create(lib, recursive = TRUE, showWarnings = FALSE)
packages <- c("DBI", "adbcdrivermanager", "DBItest", "testthat", "xml2", "bit64", "hms", "blob", "withr")
missing <- packages[!vapply(packages, requireNamespace, logical(1), quietly = TRUE)]
if (length(missing)) install.packages(missing, lib = lib, repos = "https://cloud.r-project.org")
# nanoarrow 0.9 converts int64 timestamps to double before applying their
# unit, issuing a lossy-integer warning even for exactly representable POSIXct
# seconds. Divide integer ticks first, preserving the remainder separately.
nano_work <- tempfile("redis-nanoarrow-source-")
dir.create(nano_work)
nano_archive <- file.path(nano_work, "nanoarrow.tar.gz")
download.file("https://cran.r-project.org/src/contrib/nanoarrow_0.9.0.tar.gz", nano_archive)
untar(nano_archive, exdir = nano_work)
nano_source <- file.path(nano_work, "nanoarrow")
nano_path <- file.path(nano_source, "src", "materialize_posixct.h")
nano_code <- readChar(nano_path, file.info(nano_path)$size)
needle <- "if (converter->ptype_view.sexp_type == REALSXP) {"
replacement <- paste(needle,
  "    if (converter->schema_view.type == NANOARROW_TYPE_TIMESTAMP &&",
  "        converter->src.array_view->storage_type == NANOARROW_TYPE_INT64 &&",
  "        converter->schema_view.time_unit != NANOARROW_TIME_UNIT_SECOND) {",
  "      int64_t factor = converter->schema_view.time_unit == NANOARROW_TIME_UNIT_MILLI ? 1000 :",
  "          converter->schema_view.time_unit == NANOARROW_TIME_UNIT_MICRO ? 1000000 : 1000000000;",
  "      double* result = REAL(converter->dst.vec_sexp);",
  "      int64_t n_bad_values = 0;",
  "      for (int64_t i = 0; i < converter->dst.length; i++) {",
  "        int64_t index = converter->src.offset + i;",
  "        if (ArrowArrayViewIsNull(converter->src.array_view, index)) {",
  "          result[converter->dst.offset + i] = NA_REAL;",
  "        } else {",
  "          int64_t ticks = ArrowArrayViewGetIntUnsafe(converter->src.array_view, index);",
  "          int64_t whole = ticks / factor;",
  "          if (whole > MAX_DBL_AS_INTEGER || whole < -MAX_DBL_AS_INTEGER) n_bad_values++;",
  "          result[converter->dst.offset + i] = (double)(ticks / factor) + (double)(ticks % factor) / factor;",
  "        }",
  "      }",
  "      if (n_bad_values) warn_lossy_conversion(n_bad_values, \"may have incurred loss of precision in conversion to POSIXct seconds\");",
  "      return NANOARROW_OK;",
  "    }", sep = "\n")
if (!grepl(needle, nano_code, fixed = TRUE)) stop("Pinned nanoarrow timestamp patch no longer applies")
writeChar(sub(needle, replacement, nano_code, fixed = TRUE), nano_path, eos = NULL)
status <- system2(file.path(R.home("bin"), "R"), c("CMD", "INSTALL", paste0("--library=", shQuote(lib)), shQuote(nano_source)))
unlink(nano_work, recursive = TRUE)
if (status != 0L) stop("nanoarrow installation failed")
revision <- "ac5d3294597c453c8cb1c278b9536cd9afb3eae2"
work <- tempfile("redis-adbi-source-")
dir.create(work)
archive <- file.path(work, "adbi.tar.gz")
download.file(paste0("https://codeload.github.com/r-dbi/adbi/tar.gz/", revision), archive)
untar(archive, exdir = work)
source <- file.path(work, paste0("adbi-", revision))
# ADBC positional fields may have empty names. adbi 0.1.4 recognizes only
# zero-based numbers and $n, and incorrectly demands named values for "".
# Correct that upstream predicate without altering any DBItest assertions.
path <- file.path(source, "R", "dbBind_AdbiResult.R")
code <- readChar(path, file.info(path)$size)
old <- "named_schema <- all("
if (!grepl(old, code, fixed = TRUE)) stop("Pinned adbi positional-schema patch no longer applies")
code <- sub(old, "named_schema <- all(nzchar(names(schema)) &", code, fixed = TRUE)
binding <- "adbcdrivermanager::adbc_statement_bind_stream(res@statement, params)"
time_binding <- paste(
  "if (!isFALSE(schema)) {",
  "  for (i in seq_along(params)) {",
  "    if (inherits(params[[i]], 'difftime') && schema[[i]]$type %in% c('time32', 'time64')) {",
  "      params[[i]] <- structure(as.numeric(params[[i]], units = 'secs'), class = c('hms', 'difftime'), units = 'secs')",
  "    }",
  "  }",
  "}", binding, sep = "\n"
)
if (!grepl(binding, code, fixed = TRUE)) stop("Pinned adbi time-binding patch no longer applies")
code <- sub(binding, time_binding, code, fixed = TRUE)
writeChar(code, path, eos = NULL)
description <- file.path(source, "DESCRIPTION")
metadata <- cbind(read.dcf(description), "empty positional names and schema-aware time binding")
colnames(metadata)[ncol(metadata)] <- "Config/Redis-ADBC-Patch"
write.dcf(metadata, description)
status <- system2(file.path(R.home("bin"), "R"), c("CMD", "INSTALL", paste0("--library=", shQuote(lib)), shQuote(source)))
unlink(work, recursive = TRUE)
if (status != 0L) stop("adbi installation failed")
