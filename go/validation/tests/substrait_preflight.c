// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <dlfcn.h>
#include <stdio.h>
#include "adbc.h"

static void release_error(struct AdbcError* e) {
  if (e->release) e->release(e);
}
int main(int argc, char** argv) {
  assert(argc == 2);
  // Wheels need not ship Go-generated headers or retain the build filename.
  // Load the exact requested library and use the public ADBC vtable instead.
  void* library = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
  if (!library) { fprintf(stderr, "%s\n", dlerror()); return 1; }
  AdbcDriverInitFunc init = (AdbcDriverInitFunc)dlsym(library, "AdbcDriverRedisInit");
  assert(init != NULL);
  struct AdbcDriver driver = {0};
  struct AdbcError init_error = {0};
  assert(init(ADBC_VERSION_1_1_0, &driver, &init_error) == ADBC_STATUS_OK);
  struct AdbcDatabase db = {0};
  struct AdbcConnection conn = {0};
  struct AdbcStatement stmt = {0};
  struct AdbcError err = {0};
  assert(driver.DatabaseNew(&db, &err) == ADBC_STATUS_OK);
  assert(driver.DatabaseSetOption(&db, "uri", getenv("REDIS_URI"), &err) == ADBC_STATUS_OK);
  assert(driver.DatabaseInit(&db, &err) == ADBC_STATUS_OK);
  assert(driver.ConnectionNew(&conn, &err) == ADBC_STATUS_OK);
  assert(driver.ConnectionInit(&conn, &db, &err) == ADBC_STATUS_OK);
  assert(driver.StatementNew(&conn, &stmt, &err) == ADBC_STATUS_OK);
  assert(driver.StatementSetSqlQuery(&stmt, "SELECT 42", &err) == ADBC_STATUS_OK);
  // Neither address may be dereferenced, narrowed or copied.
  assert(driver.StatementSetSubstraitPlan(&stmt, (const uint8_t*)1, (size_t)1024*1024+1, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  assert(driver.StatementSetSubstraitPlan(&stmt, NULL, 1, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  assert(driver.StatementSetSubstraitPlan(&stmt, (const uint8_t*)1, SIZE_MAX, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  // Invalid plans preserve SQL and must not poison the process.
  int64_t affected = -1;
  assert(driver.StatementExecuteQuery(&stmt, NULL, &affected, &err) == ADBC_STATUS_OK);
  assert(driver.StatementRelease(&stmt, &err) == ADBC_STATUS_OK);
  assert(driver.ConnectionRelease(&conn, &err) == ADBC_STATUS_OK);
  assert(driver.DatabaseRelease(&db, &err) == ADBC_STATUS_OK);
  return 0;
}
