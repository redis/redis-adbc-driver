# DuckDB's community-extension CI uses these standard targets. Go driver
# development continues to use make -C go.
PROJ_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
EXT_NAME = redis_adbc
EXT_CONFIG = $(PROJ_DIR)extension_config.cmake
# redis_adbc is intentionally dynamically loaded by DuckDB. Ensure `require`
# installs it from the build's local repository instead of skipping the tests.
export DUCKDB_TEST_AUTOLOADING ?= available
include extension-ci-tools/makefiles/duckdb_extension.Makefile
