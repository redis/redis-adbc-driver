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
