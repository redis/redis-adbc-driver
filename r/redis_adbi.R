# Redis-specific DBI type mappings over the upstream adbi implementation.
# Source this file, then DBI::dbConnect(redis_adbi(), uri = ..., bigint = "integer64").
# The native library is selected by REDIS_ADBC_LIBRARY.
if (!requireNamespace("adbi", quietly = TRUE) || utils::packageVersion("adbi") < "0.1.4") stop("Install adbi >= 0.1.4")
if (!methods::isClass("RedisAdbiDriver")) {
  methods::setClass("RedisAdbiDriver", contains = "AdbiDriver")
  methods::setClass("RedisAdbiConnection", contains = "AdbiConnection")
}

redis_adbi <- function(library = Sys.getenv("REDIS_ADBC_LIBRARY")) {
  driver <- adbcdrivermanager::adbc_driver(library)
  methods::new("RedisAdbiDriver", adbi::adbi(driver))
}

methods::setMethod(DBI::dbConnect, "RedisAdbiDriver", function(drv, ...) {
  methods::new("RedisAdbiConnection", DBI::dbConnect(methods::as(drv, "AdbiDriver"), ...))
})

redis_data_type <- function(object) {
  if (inherits(object, "integer64")) return("BIGINT")
  if (inherits(object, "POSIXt")) return("TIMESTAMP WITH TIME ZONE")
  if (is.logical(object)) return("BOOLEAN")
  if (inherits(object, "difftime")) return("TIME")
  if (is.raw(object) || inherits(object, "blob") ||
      (is.list(object) && all(vapply(object, function(x) is.null(x) || is.raw(x), logical(1))))) {
    return("BYTEA")
  }
  DBI::dbDataType(DBI::ANSI(), object)
}
methods::setMethod(DBI::dbDataType, "RedisAdbiDriver", function(dbObj, obj, ...) redis_data_type(obj))
methods::setMethod(DBI::dbDataType, "RedisAdbiConnection", function(dbObj, obj, ...) redis_data_type(obj))

redis_frame <- function(value) {
  if (!is.data.frame(value)) stop("value must be a data frame")
  for (name in names(value)) {
    x <- value[[name]]
    if (is.raw(x)) value[[name]] <- lapply(x, function(x) as.raw(x))
    if (inherits(x, "AsIs") && is.list(x) &&
        all(vapply(x, function(item) is.null(item) || is.raw(item), logical(1)))) {
      value[[name]] <- unclass(x)
    }
    if (inherits(x, "difftime")) value[[name]] <- hms::as_hms(x)
  }
  value
}

redis_write_table <- function(conn, name, value, overwrite = FALSE, append = FALSE,
                              ..., field.types = NULL, row.names = NULL, temporary = FALSE) {
  value <- redis_frame(value)
  if (any(!vapply(list(overwrite, append, temporary), function(x) is.logical(x) && length(x) == 1L && !is.na(x), logical(1)))) {
    stop("overwrite, append and temporary must be logical scalars")
  }
  if (overwrite && append) stop("overwrite and append cannot both be TRUE")
  if (append && !is.null(field.types)) stop("field.types cannot be supplied when appending")
  if (append && !DBI::dbExistsTable(conn, name)) append <- FALSE
  if (is.null(field.types)) {
    base <- methods::selectMethod(DBI::dbWriteTable, c("AdbiConnection", "Id", "data.frame"))
    return(base(conn, name, value, overwrite = overwrite, append = append,
                row.names = row.names, temporary = temporary, ...))
  }
  value <- DBI::sqlRownamesToColumn(value, row.names)
  if (!is.character(field.types) || anyNA(field.types) || is.null(names(field.types)) ||
      anyDuplicated(names(field.types)) || any(!names(field.types) %in% names(value))) stop("Invalid field.types names")
  types <- vapply(value, redis_data_type, character(1))
  types[names(field.types)] <- field.types
  if (overwrite && DBI::dbExistsTable(conn, name)) DBI::dbRemoveTable(conn, name, temporary = temporary)
  if (!append) DBI::dbExecute(conn, DBI::sqlCreateTable(conn, name, types, row.names = FALSE, temporary = temporary))
  if (nrow(value)) DBI::dbAppendTable(conn, name, value)
  invisible(TRUE)
}
methods::setMethod(DBI::dbWriteTable, c("RedisAdbiConnection", "Id", "data.frame"), redis_write_table)
methods::setMethod(DBI::dbWriteTable, c("RedisAdbiConnection", "character", "data.frame"), function(conn, name, value, ...) {
  redis_write_table(conn, DBI::Id(table = name), value, ...)
})
methods::setMethod(DBI::dbWriteTable, c("RedisAdbiConnection", "SQL", "data.frame"), function(conn, name, value, ...) {
  redis_write_table(conn, DBI::dbUnquoteIdentifier(conn, name)[[1L]], value, ...)
})

methods::setMethod(DBI::dbAppendTable, "RedisAdbiConnection", function(conn, name, value, ..., row.names = NULL) {
  if (!is.null(row.names)) stop("row.names cannot be supplied to dbAppendTable")
  base <- methods::selectMethod(DBI::dbAppendTable, "AdbiConnection")
  base(conn, name, redis_frame(value), ...)
})

methods::setMethod(DBI::dbListTables, "RedisAdbiConnection", function(conn, ...) {
  DBI::dbGetQuery(conn, paste(
    "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid",
    "WHERE c.relkind IN ('r', 'v') AND pg_catalog.pg_table_is_visible(c.oid)",
    "AND n.nspname NOT IN ('pg_catalog', 'information_schema') ORDER BY c.relname"
  ))[[1L]]
})
redis_exists_table <- function(conn, name, ...) {
  tryCatch({
    DBI::dbGetQuery(conn, paste("SELECT * FROM", DBI::dbQuoteIdentifier(conn, name), "LIMIT 0"))
    TRUE
  }, adbc_status_not_found = function(error) FALSE)
}
methods::setMethod(DBI::dbExistsTable, c("RedisAdbiConnection", "character"), redis_exists_table)
methods::setMethod(DBI::dbExistsTable, c("RedisAdbiConnection", "Id"), redis_exists_table)
methods::setMethod(DBI::dbExistsTable, c("RedisAdbiConnection", "SQL"), redis_exists_table)

methods::setMethod(DBI::dbQuoteLiteral, c("RedisAdbiConnection", "difftime"), function(conn, x, ...) {
  value <- format(hms::as_hms(x))
  DBI::SQL(ifelse(is.na(x), "NULL", paste0("TIME '", value, "'")))
})

methods::setMethod(DBI::dbQuoteLiteral, c("RedisAdbiConnection", "POSIXt"), function(conn, x, ...) {
  value <- format(as.POSIXct(x), tz = "UTC", format = "%Y-%m-%d %H:%M:%OS6+00")
  DBI::SQL(ifelse(is.na(x), "NULL", paste0("TIMESTAMPTZ '", value, "'")))
})

methods::setMethod(DBI::dbRemoveTable, c("RedisAdbiConnection", "character"), function(conn, name, ...) {
  DBI::dbRemoveTable(conn, DBI::Id(table = name), ...)
})
methods::setMethod(DBI::dbRemoveTable, c("RedisAdbiConnection", "SQL"), function(conn, name, ...) {
  DBI::dbRemoveTable(conn, DBI::dbUnquoteIdentifier(conn, name)[[1L]], ...)
})
methods::setMethod(DBI::dbRemoveTable, c("RedisAdbiConnection", "Id"), function(conn, name, ...,
                      temporary = FALSE, fail_if_missing = TRUE) {
  if (temporary && !"schema" %in% names(name@name)) name <- DBI::Id(schema = "pg_temp", table = unname(name@name[["table"]]))
  DBI::dbExecute(conn, paste("DROP TABLE", if (!fail_if_missing) "IF EXISTS", DBI::dbQuoteIdentifier(conn, name)))
  invisible(TRUE)
})
