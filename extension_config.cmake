# Go's embedded runtime uses initial-exec TLS on Linux. A statically linked
# jemalloc adds its 2.7 KiB TLS block to the same shared object, exceeding
# glibc's dlopen static-TLS budget. Use the system allocator so the standalone
# extension can load in an ordinary DuckDB process without LD_PRELOAD/tunables.
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
  set(ENABLE_JEMALLOC OFF CACHE BOOL "Use the system allocator with embedded Go" FORCE)
endif()

if(COMMAND duckdb_extension_statically_link)
  # DuckDB 2.0 loads extensions dynamically by default and removed DONT_LINK.
  set(REDIS_ADBC_LINK_OPTIONS "")
else()
  set(REDIS_ADBC_LINK_OPTIONS DONT_LINK)
endif()
duckdb_extension_load(redis_adbc
  SOURCE_DIR ${CMAKE_CURRENT_LIST_DIR}
  LOAD_TESTS
  ${REDIS_ADBC_LINK_OPTIONS}
  EXTENSION_VERSION v0.1.0
)
